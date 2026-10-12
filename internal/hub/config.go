// Package hub — сервис-сводка по сети ресторанов: опрашивает API экземпляров read-only ключами.
package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// Point — одна точка сети (один экземпляр SwapOwl).
type Point struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	APIKey    string `json:"api_key"`
	APIKeyEnv string `json:"api_key_env"` // имя переменной окружения с ключом вместо api_key
	// Label — подпись точки в сравнении («Со SwapOwl», «Без подсказок»); необязательна.
	Label string `json:"label"`
	// Group — необязательная группа точек; «ab» — пара для живого A/B-сравнения.
	Group string `json:"group"`
}

// Config — список точек.
type Config struct {
	Points []Point
}

// LoadConfig читает файл (JSON или YAML-подмножество, см. parseYAML) и проверяет точки.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("hub: read config: %w", err)
	}
	return ParseConfig(raw, os.Getenv)
}

// ParseConfig разбирает содержимое конфига; getenv подставляет ключи из api_key_env.
// Текст ошибок не содержит значений ключей.
func ParseConfig(raw []byte, getenv func(string) string) (Config, error) {
	var pts []Point
	trimmed := strings.TrimSpace(string(raw))
	switch {
	case strings.HasPrefix(trimmed, "["):
		if err := json.Unmarshal([]byte(trimmed), &pts); err != nil {
			return Config{}, fmt.Errorf("hub: config json: %w", err)
		}
	case strings.HasPrefix(trimmed, "{"):
		var wrap struct {
			Points []Point `json:"points"`
		}
		if err := json.Unmarshal([]byte(trimmed), &wrap); err != nil {
			return Config{}, fmt.Errorf("hub: config json: %w", err)
		}
		pts = wrap.Points
	default:
		var err error
		if pts, err = parseYAML(trimmed); err != nil {
			return Config{}, err
		}
	}
	seen := map[string]bool{}
	for i := range pts {
		p := &pts[i]
		if p.APIKey == "" && p.APIKeyEnv != "" {
			p.APIKey = getenv(p.APIKeyEnv)
		}
		switch {
		case p.ID == "" || strings.ContainsAny(p.ID, "/ ?#"):
			return Config{}, fmt.Errorf("hub: point #%d: bad id", i+1)
		case seen[p.ID]:
			return Config{}, fmt.Errorf("hub: duplicate point id %q", p.ID)
		case p.APIKey == "":
			return Config{}, fmt.Errorf("hub: point %q: empty api_key", p.ID)
		}
		u, err := url.Parse(p.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return Config{}, fmt.Errorf("hub: point %q: bad url", p.ID)
		}
		p.URL = strings.TrimRight(p.URL, "/")
		if p.Name == "" {
			p.Name = p.ID
		}
		p.Label = strings.TrimSpace(p.Label)
		p.Group = strings.ToLower(strings.TrimSpace(p.Group))
		seen[p.ID] = true
	}
	ab := 0
	for _, p := range pts {
		if p.Group == GroupAB {
			ab++
		}
	}
	if ab > 2 {
		return Config{}, fmt.Errorf("hub: group %q must have at most 2 points, got %d", GroupAB, ab)
	}
	if len(pts) == 0 {
		return Config{}, errors.New("hub: config has no points")
	}
	return Config{Points: pts}, nil
}

// parseYAML понимает только список точек: «- id: x» и строки «key: value» с отступом;
// значения — скаляры (можно в кавычках), «#» начинает комментарий вне кавычек в начале строки.
// Допустим и верхний ключ «points:».
func parseYAML(s string) ([]Point, error) {
	var pts []Point
	var cur *Point
	for n, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || t == "points:" {
			continue
		}
		if strings.HasPrefix(t, "- ") {
			pts = append(pts, Point{})
			cur = &pts[len(pts)-1]
			t = strings.TrimSpace(t[2:])
		}
		k, v, ok := strings.Cut(t, ":")
		if !ok || cur == nil {
			return nil, fmt.Errorf("hub: config yaml: line %d: unsupported syntax", n+1)
		}
		v = strings.TrimSpace(v)
		if v != "" && v[0] != '"' && v[0] != '\'' {
			if i := strings.Index(v, " #"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
		}
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		switch strings.TrimSpace(k) {
		case "id":
			cur.ID = v
		case "name":
			cur.Name = v
		case "url":
			cur.URL = v
		case "api_key":
			cur.APIKey = v
		case "api_key_env":
			cur.APIKeyEnv = v
		case "label":
			cur.Label = v
		case "group":
			cur.Group = v
		}
	}
	return pts, nil
}

// PollInterval разбирает HUB_POLL (длительность Go или секунды); пусто — 60 с.
func PollInterval(s string) (time.Duration, error) {
	return parseInterval(s, 60*time.Second, "HUB_POLL")
}

// ABPollInterval разбирает HUB_AB_POLL — период опроса точек группы ab; пусто — 2 с.
func ABPollInterval(s string) (time.Duration, error) {
	return parseInterval(s, 2*time.Second, "HUB_AB_POLL")
}

func parseInterval(s string, def time.Duration, name string) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		var sec int
		if _, e := fmt.Sscanf(s, "%d", &sec); e != nil || fmt.Sprint(sec) != s {
			return 0, fmt.Errorf("hub: bad %s %q", name, s)
		}
		d = time.Duration(sec) * time.Second
	}
	if d <= 0 {
		return 0, fmt.Errorf("hub: %s must be positive", name)
	}
	return d, nil
}
