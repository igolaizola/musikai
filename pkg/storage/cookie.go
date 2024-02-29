package storage

import (
	"context"
	"fmt"
)

func (s *Store) NewCookieStore(provider, account string) *cookieStore {
	return &cookieStore{
		store:    s,
		provider: provider,
		account:  account,
	}
}

type cookieStore struct {
	store    *Store
	provider string
	account  string
}

func (c *cookieStore) GetCookie(ctx context.Context) (string, error) {
	setting, err := c.store.GetSetting(ctx, fmt.Sprintf("%s/%s/cookie", c.provider, c.account))
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (c *cookieStore) SetCookie(ctx context.Context, cookie string) error {
	return c.store.SetSetting(ctx, &Setting{
		ID:    fmt.Sprintf("%s/%s/cookie", c.provider, c.account),
		Value: cookie,
	})
}
