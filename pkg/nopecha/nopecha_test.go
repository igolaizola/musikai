package nopecha

import (
	"context"
	"testing"
)

func TestToken(t *testing.T) {
	t.Skip("Only works with a valid key")

	client, _ := New(&Config{
		Debug: true,
		Key:   "",
	})
	siteKey := "2945592b-1928-43a9-8473-7e7fed3d752e"
	u := "https://www.udio.com/"
	token, err := client.Token(context.Background(), "hcaptcha", siteKey, u)
	if err != nil {
		t.Errorf("Token() error = %v", err)
	}
	t.Log(token)
}
