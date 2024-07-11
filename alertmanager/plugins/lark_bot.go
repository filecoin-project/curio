package plugins

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/samber/lo"
)

// LarkCustomBot is a plugin for sending alerts to Lark Custom Bot.
// More information about Lark Custom Bot can be found at
// https://open.larksuite.com/document/client-docs/bot-v3/add-custom-bot
type LarkCustomBot struct {
	cfg config.LarkBotConfig
}

func (p *LarkCustomBot) SendAlert(data *AlertPayload) error {
	if p.cfg.WebhookURL == "" {
		log.Warnf("WebhookURL is not set, skipping alert.")
		return nil
	}
	type botData struct {
		Timestamp int64    `json:"timestamp,omitempty"`
		Sign      string   `json:"sign,omitempty"`
		MsgType   string   `json:"msg_type"`
		Card      larkCard `json:"card"`
	}

	payload := &botData{
		MsgType: "interactive",
		Card: larkCard{
			Header: larkHeader{
				Template: "red",
				Title: larkText{
					Content: fmt.Sprintf("%s %s - %s", data.Summary, data.Severity, data.Source),
					Tag:     "plain_text",
				},
			},
			Elements: []larkElement{
				{
					Tag:             "column_set",
					FlexMode:        "none",
					BackgroundStyle: "default",
					Columns: []larkColumn{
						{
							Tag:           "column",
							Width:         "weighted",
							Weight:        1,
							VerticalAlign: "top",
							Elements: []larkElement{
								{
									Tag: "div",
									Text: larkText{
										Tag:     "lark_md",
										Content: fmt.Sprintf("**🔴 Project:**\n%s", data.Source),
									},
								},
							},
						},
						{
							Tag:           "column",
							Width:         "weighted",
							Weight:        1,
							VerticalAlign: "top",
							Elements: []larkElement{
								{
									Tag: "div",
									Text: larkText{
										Tag:     "lark_md",
										Content: fmt.Sprintf("**🕐 Time:**\n%s", data.Time.Format(time.RFC3339)),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if len(p.cfg.AtUserIds) > 0 {
		elements := larkElement{
			Tag:             "column_set",
			FlexMode:        "none",
			BackgroundStyle: "default",
		}
		for idx, oid := range p.cfg.AtUserIds {
			elements.Columns = append(elements.Columns, larkColumn{
				Tag:           "column",
				Width:         "weighted",
				Weight:        1,
				VerticalAlign: "top",
				Elements: []larkElement{
					{
						Tag: "div",
						Text: larkText{
							Tag:     "lark_md",
							Content: fmt.Sprintf("**👤 Level %d on call:**\n<at id=%s></at>", idx+1, oid),
						},
					},
				},
			})
			if len(elements.Columns) == 2 {
				payload.Card.Elements = append(payload.Card.Elements, elements)
				elements = larkElement{
					Tag:             "column_set",
					FlexMode:        "none",
					BackgroundStyle: "default",
				}
			}
		}
		if len(elements.Columns) > 0 {
			payload.Card.Elements = append(payload.Card.Elements, elements)
		}
	}

	for k, v := range data.Details {
		payload.Card.Elements = append(payload.Card.Elements, larkElement{
			Tag: "div",
			Text: larkText{
				Tag:     "lark_md",
				Content: fmt.Sprintf("**%s :**\n%s", k, v),
			},
		})
	}
	if p.cfg.Footer != "" {
		payload.Card.Elements = append(payload.Card.Elements, larkElement{
			Tag: "hr",
		})
		payload.Card.Elements = append(payload.Card.Elements, larkElement{
			Tag: "div",
			Text: larkText{
				Tag:     "lark_md",
				Content: p.cfg.Footer,
			},
		})
	}

	if p.cfg.Secret != "" {
		timestamp, sign, err := p.genSign()
		if err != nil {
			return fmt.Errorf("error generating sign: %w", err)
		}
		payload.Timestamp = timestamp
		payload.Sign = sign
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	req, err := http.NewRequest("POST", p.cfg.WebhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 15 * time.Second,
	}
	iter, duration, err := lo.AttemptWithDelay(5, time.Second,
		func(i int, duration time.Duration) error {
			resp, err := client.Do(req)
			if err != nil {
				time.Sleep(duration * time.Duration(i))
				return err
			}
			defer func() { _ = resp.Body.Close() }()

			switch resp.StatusCode {
			case http.StatusOK:
				type response struct {
					Code int    `json:"code"`
					Msg  string `json:"msg"`
				}
				var r response
				err = json.NewDecoder(resp.Body).Decode(&r)
				if err != nil {
					return fmt.Errorf("error decoding response: %v", err)
				}
				if r.Code != 0 {
					return fmt.Errorf("error response: %v", r)
				}
				return nil
			default:
				return fmt.Errorf("unexpected HTTP response: %v", resp.Status)
			}
		},
	)
	if err != nil {
		log.Errorw("error sending alert", "retry", iter, "duration", duration, "error", err)
		return err
	}
	return nil
}

func (p *LarkCustomBot) genSign() (int64, string, error) {
	timestamp := time.Now().Unix()
	stringToSign := fmt.Sprintf("%v", timestamp) + "\n" + p.cfg.Secret
	var data []byte
	h := hmac.New(sha256.New, []byte(stringToSign))
	_, err := h.Write(data)
	if err != nil {
		return 0, "", err
	}
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return timestamp, signature, nil
}

type larkCard struct {
	Elements []larkElement `json:"elements"`
	Header   larkHeader    `json:"header"`
}

type larkHeader struct {
	Template string   `json:"template"`
	Title    larkText `json:"title"`
}

type larkText struct {
	Content string `json:"content"`
	Tag     string `json:"tag"`
}

type larkColumn struct {
	Tag           string        `json:"tag"`
	Width         string        `json:"width"`
	Weight        int           `json:"weight"`
	VerticalAlign string        `json:"vertical_align"`
	Elements      []larkElement `json:"elements"`
}

type larkElement struct {
	Tag             string       `json:"tag"`
	FlexMode        string       `json:"flex_mode,omitempty"`
	BackgroundStyle string       `json:"background_style,omitempty"`
	Columns         []larkColumn `json:"columns,omitempty"`
	Text            larkText     `json:"text,omitempty"`
	Elements        []larkText   `json:"elements,omitempty"`
}
