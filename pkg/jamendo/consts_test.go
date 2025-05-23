package jamendo

import (
	"fmt"
	"testing"

	"github.com/igolaizola/musikai/pkg/sonoteller"
)

func TestSonoteller(t *testing.T) {
	var notFound int
	var total int

	fields := map[string][]string{
		"genre":      sonoteller.Genres,
		"instrument": sonoteller.Instruments,
		"mood":       sonoteller.Moods,
		"style":      sonoteller.Styles,
	}
	for f, vs := range fields {
		fmt.Println("# Field:", f)
		total += len(vs)
		for _, v := range vs {
			_, _, ok := GetField(v)
			if !ok {
				fmt.Println(v)
				continue
			}
			notFound++
		}
	}
	t.Log("total:", total)
	t.Log("not found:", notFound)
}
