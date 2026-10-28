package fonts

import (
	"context"
	"math/rand"
	"testing"
)

// Damaged fonts fail with an error, never a panic or a hang.
func TestDamagedFontsFailCleanly(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	var inputs [][]byte
	for _, s := range []string{trueTypeSample, cidSample, nameKeyed, googleWoff2} {
		inputs = append(inputs, sample(t, s))
	}
	src := mustLoad(t, sample(t, trueTypeSample))
	inputs = append(inputs, mustSave(t, src, "woff"), mustSave(t, src, "woff2"), mustSave(t, src, "eot"), fonKinds[0].data())
	for _, in := range inputs {
		for i := 0; i < 60; i++ {
			b := append([]byte(nil), in...)
			for n := rng.Intn(8) + 1; n > 0; n-- {
				b[rng.Intn(len(b))] = byte(rng.Intn(256))
			}
			var err error
			func() {
				defer recoverData(&err)
				if b[0] == 'M' {
					for _, s := range readFon(context.Background(), b) {
						saveFont(context.Background(), buildFon(context.Background(), &s, 1, 1), "woff2", nil)
					}
					return
				}
				f := loadFont(context.Background(), b)
				for _, format := range Formats {
					saveFont(context.Background(), f, format, nil)
				}
			}()
		}
	}
}
