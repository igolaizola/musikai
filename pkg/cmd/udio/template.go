package udio

import (
	"fmt"
	"math/rand"
)

type template struct {
	Type string `json:"type,omitempty"`

	Prompt       string `json:"prompt,omitempty"`
	Style        string `json:"style,omitempty"`
	Title        string `json:"title,omitempty"`
	Instrumental bool   `json:"instrumental,omitempty"`
}

func newPrompt(typ, prompt string, instr bool) template {
	return template{
		Type:         typ,
		Prompt:       prompt,
		Instrumental: instr,
	}
}

func newStyle(typ, style string, instr bool) template {
	return template{
		Type:         typ,
		Style:        style,
		Instrumental: instr,
	}
}

func (t template) String() string {
	return fmt.Sprintf("%s, p: %s, s: %s, t: %s, i: %v}",
		t.Type, t.Prompt, t.Style, t.Title, t.Instrumental)
}

func nextTemplate() template {
	var opts []template
	opts = append(opts, options(100, newPrompt("lullaby", "genre: lullaby", true))...)
	opts = append(opts, options(80, newPrompt("classical", "genre: classical", true))...)
	opts = append(opts, options(80, newPrompt("jazz", "genre: jazz", true))...)
	opts = append(opts, options(50, newPrompt("post-metal", "genre: post-metal", true))...)
	opts = append(opts, options(50, newPrompt("edm", "genre: electronic dance", true))...)
	opts = append(opts, options(20, newPrompt("post-rock", "genre: post-rock", true))...)
	opts = append(opts, options(10, newPrompt("post-punk", "genre: post-punk", true))...)
	opts = append(opts, options(10, newPrompt("bluegrass", "genre: bluegrass", true))...)
	opts = append(opts, options(10, newPrompt("ambient", "genre: ambient", true))...)
	opts = append(opts, options(10, newPrompt("film score", "genre: film score", true))...)
	opts = append(opts, options(10, newPrompt("lo-fi", "genre: lo-fi", true))...)
	opts = append(opts, options(10, newStyle("daftpunk", "electronic, funk, disco, house, synth-pop, innovative", true))...)
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
