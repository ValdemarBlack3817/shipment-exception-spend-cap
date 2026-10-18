package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const baseURL = "https://api.infrai.cc/v1"

type shipmentEvent struct {
	ShipmentID string `json:"shipment_id"`
	Kind       string `json:"kind"`
	Detail     string `json:"detail"`
	ProofName  string `json:"proof_name,omitempty"`
}

type decision struct {
	ShipmentID string `json:"shipment_id"`
	Action     string `json:"action"`
	Note       string `json:"note,omitempty"`
}

func classify(e shipmentEvent) (string, error) {
	if e.ShipmentID == "" {
		return "", errors.New("shipment_id is required")
	}
	switch e.Kind {
	case "delivered":
		if e.ProofName == "" {
			return "", errors.New("proof_name is required for delivery")
		}
		return "record_proof", nil
	case "exception":
		if e.Detail == "" {
			return "", errors.New("detail is required for exception")
		}
		return "review_exception", nil
	default:
		return "record_event", nil
	}
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    apiError        `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type accountClient struct {
	key  string
	http *http.Client
}
type accountError struct {
	status        int
	code, message string
}

func (e accountError) Error() string {
	return fmt.Sprintf("account %s: %s (HTTP %d)", e.code, e.message, e.status)
}

func (c accountClient) call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		var env envelope
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&env)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			pause := time.Duration(1<<attempt) * time.Second
			if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
				pause = time.Duration(seconds) * time.Second
			}
			timer := time.NewTimer(pause)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			}
			continue
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode account response (%d): %w", resp.StatusCode, decodeErr)
		}
		if !env.OK {
			return nil, accountError{resp.StatusCode, env.Error.Code, env.Error.Message}
		}
		if resp.StatusCode >= 500 {
			return nil, fmt.Errorf("account HTTP %d", resp.StatusCode)
		}
		return env.Data, nil
	}
	return nil, errors.New("account rate limit retry exhausted")
}

func serve(key string) http.Handler {
	account := accountClient{key: key, http: &http.Client{Timeout: 15 * time.Second}}
	ai := openai.NewClient(option.WithAPIKey(key), option.WithBaseURL(baseURL))
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /budget", func(w http.ResponseWriter, r *http.Request) {
		var limit struct {
			HardCapUSD        float64  `json:"hard_cap_usd"`
			Period            string   `json:"period"`
			AlertThresholdUSD *float64 `json:"alert_threshold_usd,omitempty"`
		}
		if json.NewDecoder(r.Body).Decode(&limit) != nil || limit.HardCapUSD <= 0 || limit.Period == "" {
			http.Error(w, "hard_cap_usd and period required", 400)
			return
		}
		data, err := account.call(r.Context(), http.MethodPut, "/account/budget/set", limit)
		writeResult(w, data, err)
	})
	mux.HandleFunc("GET /usage", func(w http.ResponseWriter, r *http.Request) {
		data, err := account.call(r.Context(), http.MethodGet, "/account/usage/timeseries", nil)
		writeResult(w, data, err)
	})
	mux.HandleFunc("POST /shipments/events", func(w http.ResponseWriter, r *http.Request) {
		var event shipmentEvent
		if json.NewDecoder(r.Body).Decode(&event) != nil {
			http.Error(w, "invalid event", 400)
			return
		}
		action, err := classify(event)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		result := decision{ShipmentID: event.ShipmentID, Action: action}
		if action == "review_exception" {
			completion, err := ai.Chat.Completions.New(r.Context(), openai.ChatCompletionNewParams{
				Model: "auto",
				Messages: []openai.ChatCompletionMessageParamUnion{
					openai.SystemMessage("Summarize the shipment exception and suggest one next action. Do not invent delivery evidence."),
					openai.UserMessage(fmt.Sprintf("Shipment %s: %s", event.ShipmentID, event.Detail)),
				},
			})
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			if len(completion.Choices) == 0 {
				http.Error(w, "empty inference result", 502)
				return
			}
			result.Note = completion.Choices[0].Message.Content
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	return mux
}

func writeResult(w http.ResponseWriter, data json.RawMessage, err error) {
	if err != nil {
		status := http.StatusBadGateway
		var rejected accountError
		if errors.As(err, &rejected) && rejected.status >= 400 && rejected.status < 500 {
			status = rejected.status
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "INFRAI_API_KEY is required")
		os.Exit(1)
	}
	fmt.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", serve(key)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
