package generate

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

func (t template) String() string {
	return fmt.Sprintf("%s, p: %s, s: %s, t: %s, i: %v}",
		t.Type, t.Prompt, t.Style, t.Title, t.Instrumental)
}

func nextTemplate() template {
	var opts []string
	opts = append(opts, options(100, "lullaby")...)
	opts = append(opts, options(80, "classical")...)
	opts = append(opts, options(80, "jazz")...)
	opts = append(opts, options(50, "post-metal")...)
	opts = append(opts, options(50, "electronic dance")...)
	opts = append(opts, options(20, "post-rock")...)
	opts = append(opts, options(10, "post-punk")...)
	opts = append(opts, options(10, "bluegrass")...)
	opts = append(opts, options(10, "ambient")...)
	opts = append(opts, options(10, "film score")...)
	opts = append(opts, options(10, "lo-fi")...)
	genre := opts[rand.Intn(len(opts))]

	return template{
		Type:         genre,
		Prompt:       fmt.Sprintf("genre: %s", genre),
		Instrumental: true,
	}
}

func options(n int, v string) []string {
	var opts []string
	for i := 0; i < n; i++ {
		opts = append(opts, v)
	}
	return opts
}
