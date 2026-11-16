package params

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// ErrRelayRefused identifies an explicit server response that must not use stale cached links.
var ErrRelayRefused = errors.New("служба комнат отказала")

type RelayLinks struct {
	VK   []string `json:"vk"`
	Dion []string `json:"dion"`
}

func relayLinksEndpoint(a Address) (string, bool) {
	if a.Pinned {
		return "", false
	}
	if a.Host == "cdn.jincomputers.win" && a.Port == 8443 {
		return "https://cdn.jincomputers.win:8443/v1/relay-links", true
	}
	if (a.Host == "cdn.jincomputers.win" || a.Host == "api-ru.meridianvpn.org" || a.Host == "meridian-ru-reserve-8d8e90b95b.bioton2.workers.dev") && a.Port == 443 {
		return "https://" + a.Host + "/v1/relay-links", true
	}
	return "", false
}

// FetchRelayLinks uses the separately specified JSON contract, through the
// trusted CDN or backup hostname only. Credentials are sent in the POST body.
func (c *Client) FetchRelayLinks(ctx context.Context, key, device string) (*RelayLinks, error) {
	if len(c.cfg.Addresses) != 1 {
		return nil, errors.New("нет отдельного адреса API")
	}
	a := c.cfg.Addresses[0]
	u, trusted := relayLinksEndpoint(a)
	if !trusted {
		return nil, errors.New("неподходящий адрес API")
	}
	body, err := json.Marshal(map[string]string{"key": key, "device_id": device})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("не удалось подготовить запрос комнат")
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.httpClient(a)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("служба комнат недоступна")
	}
	defer resp.Body.Close()
	return decodeRelayLinksResponse(resp)
}

func decodeRelayLinksResponse(resp *http.Response) (*RelayLinks, error) {
	if resp.StatusCode != http.StatusOK {
		return nil, ErrRelayRefused
	}
	var reply struct {
		Status string `json:"status"`
		Links  *struct {
			VK   *[]string `json:"vk"`
			Dion *[]string `json:"dion"`
		} `json:"links"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&reply); err != nil || reply.Status != "ok" || reply.Links == nil {
		return nil, ErrRelayRefused
	}
	if reply.Links.VK == nil || reply.Links.Dion == nil {
		return nil, ErrRelayRefused
	}
	if len(*reply.Links.VK) > 5 || len(*reply.Links.Dion) > 3 {
		return nil, ErrRelayRefused
	}
	return &RelayLinks{VK: *reply.Links.VK, Dion: *reply.Links.Dion}, nil
}
