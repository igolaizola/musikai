package jamendo

import (
	"strings"
	"testing"

	"github.com/igolaizola/musikai/pkg/sonoteller"
)

func TestSonoteller(t *testing.T) {
	tags := toMap(Tags)
	genres := toMap(Genres)

	var notFound int
	var total int

	fields := map[string][]string{
		"genre":      sonoteller.Genres,
		"instrument": sonoteller.Instruments,
		"mood":       sonoteller.Moods,
		"style":      sonoteller.Styles,
	}
	for f, vs := range fields {
		total += len(vs)
		for _, v := range vs {
			v = strings.ToLower(v)
			if c, ok := convert[v]; ok {
				v = c
			}
			v = strings.ToLower(v)
			_, ok1 := genres[v]
			_, ok2 := tags[v]
			if !ok1 && !ok2 {
				t.Errorf("%s %q not found", f, v)
				continue
			}
			notFound++

		}
	}
	t.Log("total:", total)
	t.Log("not found:", notFound)
}

func toMap(v []string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, s := range v {
		s = strings.ToLower(s)
		m[s] = struct{}{}
	}
	return m
}
