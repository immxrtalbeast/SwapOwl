package invariants

import (
	"math/rand"
	"testing"
	"time"

	"umirhack/internal/hotoffers"
)

func place(offers []hotoffers.Offer, dish string) int {
	for i, o := range offers {
		if o.DishID == dish {
			return i
		}
	}
	return -1
}

// Инвариант 11. Монотонность по сроку: если срок партии приблизился (не пересекая порог
// «обречённой»), место блюда с её продуктом не ухудшилось.
func TestInv11MonotoneInExpiry(t *testing.T) {
	for name, rank := range algos {
		t.Run(name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(11))
			for iter := 0; iter < 500; iter++ {
				in := randomInput(rng)
				// Двигаем только партии, у которых после сдвига остаётся не меньше суток до срока:
				// порог W_min заведомо не пересекается.
				idx := rng.Intn(len(in.Batches))
				left := in.Batches[idx].ExpiresAt.Sub(in.Now)
				if left < 30*time.Hour {
					continue
				}
				before, _ := rank(cloneInput(in))
				moved := cloneInput(in)
				moved.Batches[idx].ExpiresAt = in.Now.Add(left - time.Duration(1+rng.Int63n(int64((left-24*time.Hour)/time.Hour)))*time.Hour)
				after, _ := rank(moved)

				prod := in.Batches[idx].ProductID
				for _, d := range in.Dishes {
					uses := false
					for _, r := range d.Recipe {
						uses = uses || r.ProductID == prod
					}
					pb := place(before, d.ID)
					if !uses || pb < 0 {
						continue
					}
					if pa := place(after, d.ID); pa < 0 || pa > pb {
						t.Fatalf("итер %d: блюдо %s с продуктом %s было на месте %d, после приближения срока — %d",
							iter, d.ID, prod, pb, pa)
					}
				}
			}
		})
	}
}
