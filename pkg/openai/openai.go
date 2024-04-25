package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/sashabaranov/go-openai"
)

type Client struct {
	debug  bool
	client *openai.Client
	model  string
}

type Config struct {
	Debug bool
	Token string
	Host  string
	Model string
}

func New(cfg *Config) *Client {
	model := cfg.Model
	if model == "" {
		model = openai.GPT3Dot5Turbo
	}
	var client *openai.Client
	if cfg.Host == "" {
		client = openai.NewClient(cfg.Token)
	} else {
		openaiConfig := openai.DefaultConfig(cfg.Token)
		openaiConfig.BaseURL = cfg.Host
		client = openai.NewClientWithConfig(openaiConfig)
	}
	return &Client{
		debug:  cfg.Debug,
		client: client,
		model:  model,
	}
}

func (c *Client) ChatCompletion(ctx context.Context, msg string) (string, error) {
	req := openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: msg,
			},
		},
		MaxTokens: 1024,
	}
	if c.debug {
		js, _ := json.MarshalIndent(req, "", "  ")
		log.Println("openai: req:", string(js))
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("openai: couldn't create chat completion: %w", err)
	}
	if c.debug {
		js, _ := json.MarshalIndent(resp, "", "  ")
		log.Println("openai: resp:", string(js))
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai: chat completion response is empty")
	}
	content := resp.Choices[0].Message.Content
	if content == "" {
		return "", fmt.Errorf("openai: chat completion response is empty")
	}
	return content, nil
}

/*


if err != nil {
	fmt.Printf("ChatCompletion error: %v\n", err)
	return
}

fmt.Println(resp.Choices[0].Message.Content)
*/
