package tgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api"
)

type Store struct {
	bot    *tgbot.BotAPI
	token  string
	chat   int64
	client *http.Client
	debug  bool
}

func New(token string, chat int64, proxy string, debug bool) (*Store, error) {
	bot, err := tgbot.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	// Check that chatID is valid
	if _, err := bot.GetChat(tgbot.ChatConfig{ChatID: chat}); err != nil {
		return nil, fmt.Errorf("tgstore: invalid chat id: %w", err)
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
	}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("tgstore: invalid proxy %s: %w", proxy, err)
		}
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(u),
		}
	}
	return &Store{
		bot:    bot,
		token:  token,
		chat:   chat,
		client: client,
		debug:  debug,
	}, nil
}

func (s *Store) Start(ctx context.Context) error {
	return nil
}

func (s *Store) Stop(ctx context.Context) error {
	return nil
}

func (s *Store) Set(ctx context.Context, path string) (string, error) {
	doc := tgbot.NewDocumentUpload(s.chat, path)

	// Upload file
	maxAttempts := 3
	attempts := 0
	var msg tgbot.Message
	for {
		var err error
		msg, err = s.bot.Send(doc)
		if err == nil {
			break
		}

		// Increase attempts and check if we should stop
		attempts++
		if attempts >= maxAttempts {
			return "", fmt.Errorf("tgstore: couldn't send file: %w", err)
		}
		idx := attempts - 1
		if idx >= len(backoff) {
			idx = len(backoff) - 1
		}
		wait := backoff[idx]
		t := time.NewTimer(wait)
		if s.debug {
			log.Printf("%v (retrying in %s)\n", err, wait)
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("tgstore: send file cancelled: %w", ctx.Err())
		case <-t.C:
		}
	}
	var fileID string
	switch {
	case msg.Audio != nil && msg.Audio.FileID != "":
		fileID = msg.Audio.FileID
	case msg.Video != nil && msg.Video.FileID != "":
		fileID = msg.Video.FileID
	case msg.Voice != nil && msg.Voice.FileID != "":
		fileID = msg.Voice.FileID
	case msg.Document != nil && msg.Document.FileID != "":
		fileID = msg.Document.FileID
	case msg.Photo != nil && len(*msg.Photo) > 0:
		fileID = (*msg.Photo)[0].FileID
	}
	if fileID == "" {
		js, _ := json.Marshal(msg)
		return "", fmt.Errorf("tgstore: message doesn't contain file: %s", string(js))
	}
	return toID(s.chat, msg.MessageID, fileID), nil
}

func (s *Store) Get(ctx context.Context, id string) (string, error) {
	_, _, fileID, err := fromID(id)
	if err != nil {
		return "", err
	}
	fileConfig := tgbot.FileConfig{
		FileID: fileID,
	}
	file, err := s.bot.GetFile(fileConfig)
	if err != nil {
		return "", fmt.Errorf("tgstore: couldn't get file: %w", err)
	}
	fileURL := file.Link(s.bot.Token)
	return fileURL, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	chat, msgID, _, err := fromID(id)
	if err != nil {
		return err
	}
	deleteConfig := tgbot.DeleteMessageConfig{
		ChatID:    chat,
		MessageID: msgID,
	}
	if _, err = s.bot.DeleteMessage(deleteConfig); err != nil {
		return fmt.Errorf("tgstore: couldn't delete message: %w", err)
	}
	return nil
}

var backoff = []time.Duration{
	15 * time.Second,
	30 * time.Second,
	1 * time.Minute,
}

func (s *Store) Download(ctx context.Context, id, output string) error {
	u, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	// Download file
	maxAttempts := 3
	attempts := 0
	var b []byte
	for {
		b, err = s.download(id, u)
		if err == nil {
			break
		}

		// Increase attempts and check if we should stop
		attempts++
		if attempts >= maxAttempts {
			return err
		}
		idx := attempts - 1
		if idx >= len(backoff) {
			idx = len(backoff) - 1
		}
		wait := backoff[idx]
		t := time.NewTimer(wait)
		if s.debug {
			log.Printf("%v (retrying in %s)\n", err, wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}

	// Write to output
	if err := os.WriteFile(output, b, 0644); err != nil {
		return fmt.Errorf("tgstore: couldn't write %s: %w", output, err)
	}
	return nil
}

func (s *Store) download(id, u string) ([]byte, error) {
	resp, err := s.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("tgstore: couldn't download %s: %w", id, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tgstore: couldn't read %s: %w", id, err)
	}
	return b, nil
}

func toID(chat int64, msgID int, fileID string) string {
	return fmt.Sprintf("%d/%d/%s", chat, msgID, fileID)
}

func fromID(id string) (int64, int, string, error) {
	split := strings.Split(id, "/")
	if len(split) != 3 {
		return 0, 0, "", fmt.Errorf("tgstore: invalid id %s", id)
	}
	chat, err := strconv.Atoi(split[0])
	if err != nil {
		return 0, 0, "", fmt.Errorf("tgstore: invalid id %s: %w", id, err)
	}
	msgID, err := strconv.Atoi(split[1])
	if err != nil {
		return 0, 0, "", fmt.Errorf("tgstore: invalid id %s: %w", id, err)
	}
	fileID := split[2]
	if fileID == "" {
		return 0, 0, "", fmt.Errorf("tgstore: invalid id %s", id)
	}
	return int64(chat), msgID, fileID, nil
}
