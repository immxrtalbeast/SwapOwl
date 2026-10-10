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
				usesProd := map[string]bool{}
				for _, d := range in.Dishes {
					for _, r := range d.Recipe {
						if r.ProductID == prod {
							usesProd[d.ID] = true
						}
					}
				}
				// «При прочих равных»: блюдо d с продуктом prod может уступить место только блюдам,
				// которые тоже используют prod (их приоритет вырос от того же сдвига). Блюда без prod
				// впереди d после сдвига должны были стоять впереди d и до него.
				ahead := func(offers []hotoffers.Offer, pos int) map[string]bool {
					m := map[string]bool{}
					for _, o := range offers[:pos] {
						if !usesProd[o.DishID] {
							m[o.DishID] = true
						}
					}
					return m
				}
				for d := range usesProd {
					pb := place(before, d)
					if pb < 0 {
						continue
					}
					pa := place(after, d)
					if pa < 0 {
						pa = len(after) // выпало из списка: впереди всё, что осталось
					}
					was := ahead(before, pb)
					for o := range ahead(after, pa) {
						if !was[o] {
							t.Fatalf("итер %d: блюдо %s (продукт %s) было на месте %d, стало %d; его обогнало блюдо %s без этого продукта",
								iter, d, prod, pb, pa, o)
						}
					}
				}
			}
		})
	}
}
