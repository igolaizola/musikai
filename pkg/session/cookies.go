package session

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

type Cookie struct {
	Name  string
	Value string
}

func UnmarshalCookies(rawCookies string, edit func(*http.Cookie) *http.Cookie) ([]*http.Cookie, error) {
	var cookies []*http.Cookie
	for _, cookie := range strings.Split(rawCookies, ";") {
		cookie = strings.TrimSpace(cookie)
		if cookie == "" {
			continue
		}
		parts := strings.SplitN(cookie, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("http: invalid cookie: %v", cookie)
		}
		value := parts[1]
		// URL encode the cookie value if it contains an invalid character.
		if strings.Contains(value, "\"") {
			value = url.QueryEscape(value)
		}
		addCookie := &http.Cookie{Name: parts[0], Value: value}
		cookies = append(cookies, addCookie)
	}
	return cookies, nil
}

func MarshalCookies(cookies []*http.Cookie) string {
	var rawCookies []string
	for _, cookie := range cookies {
		rawCookies = append(rawCookies, cookie.String())
	}
	return strings.Join(rawCookies, "; ")
}

func SetCookies(c *http.Client, rawURL string, rawCookies string, edit func(*http.Cookie) *http.Cookie) error {
	if c.Jar == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return fmt.Errorf("http: failed to create cookie jar: %w", err)
		}
		c.Jar = jar
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("http: invalid url: %v", rawURL)
	}
	cookies, err := UnmarshalCookies(rawCookies, edit)
	if err != nil {
		return err
	}
	c.Jar.SetCookies(u, cookies)
	return nil
}

func GetCookies(c *http.Client, rawURL string) (string, error) {
	if c.Jar == nil {
		return "", fmt.Errorf("http: missing cookie jar")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("http: invalid url: %v", rawURL)
	}
	var cookies []string
	for _, cookie := range c.Jar.Cookies(u) {
		cookies = append(cookies, cookie.String())
	}
	return strings.Join(cookies, "; "), nil
}
