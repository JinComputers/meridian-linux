package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// FetchPartnerLinks reads the automatic relay pool from the fixed trusted API.
// It never uses an endpoint supplied by a key or disables TLS validation.
func FetchPartnerLinks(ctx context.Context, key, deviceID string) ([]string, []string, error) {
	body, _ := json.Marshal(map[string]string{"key": key, "device_id": deviceID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://cdn.jincomputers.win/v1/relay-links", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, errors.New("пул релея недоступен")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, errors.New("пул релея недоступен")
	}
	var result struct {
		Status string `json:"status"`
		Links  struct {
			VK   []string `json:"vk"`
			Dion []string `json:"dion"`
		} `json:"links"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 64<<10)).Decode(&result); err != nil {
		return nil, nil, errors.New("неверный ответ релея")
	}
	if result.Status != "ok" || len(result.Links.VK) > 5 || len(result.Links.Dion) > 3 {
		return nil, nil, errors.New("неверный список релея")
	}
	return result.Links.VK, result.Links.Dion, nil
}
