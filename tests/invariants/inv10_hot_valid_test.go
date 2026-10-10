package invariants

import (
	"math/rand"
	"reflect"
	"testing"
)

// Инвариант 10. Горячие предложения допустимы: нет блюд из стоп-листа; каждая
// рекомендация ссылается на существующую непросроченную партию с remaining > 0;
// порядок детерминирован (не зависит от порядка входных срезов).
func TestInv10HotOffersValid(t *testing.T) {
	for name, rank := range algos {
		t.Run(name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(10))
			for iter := 0; iter < 500; iter++ {
				in := randomInput(rng)
				offers, excluded := rank(cloneInput(in))
				for _, o := range offers {
					if in.Stopped[o.DishID] {
						t.Fatalf("итер %d: блюдо %s из стоп-листа в предложениях", iter, o.DishID)
					}
					if len(o.Reasons) == 0 {
						t.Fatalf("итер %d: у предложения %s нет причин", iter, o.DishID)
					}
					for _, r := range o.Reasons {
						found := false
						for _, b := range in.Batches {
							if b.ID == r.BatchID {
								found = true
								if b.Remaining <= 0 || !in.Now.Before(b.ExpiresAt) {
									t.Fatalf("итер %d: партия %d недоступна (remaining %d, expires %s, now %s)", iter, b.ID, b.Remaining, b.ExpiresAt, in.Now)
								}
								if b.ProductID != r.ProductID {
									t.Fatalf("итер %d: партия %d принадлежит %s, а не %s", iter, b.ID, b.ProductID, r.ProductID)
								}
							}
						}
						if !found {
							t.Fatalf("итер %d: причина ссылается на несуществующую партию %d", iter, r.BatchID)
						}
					}
				}
				offers2, excluded2 := rank(shuffled(rng, in))
				if !reflect.DeepEqual(offers, offers2) || !reflect.DeepEqual(excluded, excluded2) {
					t.Fatalf("итер %d: результат зависит от порядка входных срезов\n%+v\n%+v", iter, offers, offers2)
				}
			}
		})
	}
}
