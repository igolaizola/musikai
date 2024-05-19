package generate

import (
	"fmt"
	"math/rand"
)

type template struct {
	Type string `json:"type,omitempty"`

	Prompt       string `json:"prompt,omitempty"`
	Manual       bool   `json:"manual,omitempty"`
	Instrumental bool   `json:"instrumental,omitempty"`
	Lyrics       string `json:"lyrics,omitempty"`
}

func newPrompt(typ, prompt string, manual, instr bool) template {
	return template{
		Type:         typ,
		Prompt:       prompt,
		Manual:       manual,
		Instrumental: instr,
	}
}

func (t template) String() string {
	return fmt.Sprintf("{%s, p: %s, m: %v, i: %v, l: %s}",
		t.Type, t.Prompt, t.Manual, t.Instrumental, t.Lyrics)
}

func nextTemplate() template {
	var opts []template
	opts = append(opts, options(100, newPrompt("lullaby", "genre: lullaby", false, true))...)
	opts = append(opts, options(80, newPrompt("classical", "genre: classical", false, true))...)
	opts = append(opts, options(80, newPrompt("jazz", "genre: jazz", false, true))...)
	opts = append(opts, options(50, newPrompt("post-metal", "genre: post-metal", false, true))...)
	opts = append(opts, options(50, newPrompt("edm", "genre: electronic dance", false, true))...)
	opts = append(opts, options(20, newPrompt("post-rock", "genre: post-rock", false, true))...)
	opts = append(opts, options(10, newPrompt("post-punk", "genre: post-punk", false, true))...)
	opts = append(opts, options(10, newPrompt("bluegrass", "genre: bluegrass", false, true))...)
	opts = append(opts, options(10, newPrompt("ambient", "genre: ambient", false, true))...)
	opts = append(opts, options(10, newPrompt("film score", "genre: film score", false, true))...)
	opts = append(opts, options(10, newPrompt("lo-fi", "genre: lo-fi", false, true))...)
	opts = append(opts, options(10, newPrompt("daftpunk", "electronic, funk, disco, house, synth-pop, innovative", true, true))...)
	t := opts[rand.Intn(len(opts))]

	return t
}

func options(n int, t template) []template {
	var opts []template
	for i := 0; i < n; i++ {
		opts = append(opts, t)
	}
	return opts
}
