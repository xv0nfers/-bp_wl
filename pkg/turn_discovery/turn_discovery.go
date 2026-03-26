package turn_discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var fallbackTURNHosts = []string{"5.255.211.241", "5.255.211.242", "5.255.211.243", "5.255.211.245", "5.255.211.246"}

type Provider string

const (
	ProviderVK     Provider = "vk"
	ProviderYandex Provider = "yandex"
)

type Server struct {
	Host     string
	Port     string
	Username string
	Password string
}

type Result struct {
	Provider Provider
	Servers  []Server
}

type cacheEntry struct {
	ExpiresAt time.Time
	Result    Result
}

type Discoverer struct {
	mu    sync.Mutex
	cache map[string]cacheEntry
}

func New() *Discoverer {
	return &Discoverer{cache: map[string]cacheEntry{}}
}

func ParseLink(raw string) (Provider, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", err
	}
	host := strings.ToLower(u.Host)
	path := strings.Trim(u.Path, "/")
	switch {
	case strings.Contains(host, "vk.com") && strings.Contains(path, "call/join/"):
		return ProviderVK, strings.TrimPrefix(path, "call/join/"), nil
	case strings.Contains(host, "telemost.yandex.ru") && strings.HasPrefix(path, "j/"):
		return ProviderYandex, strings.TrimPrefix(path, "j/"), nil
	default:
		return "", "", fmt.Errorf("unsupported link: %s", raw)
	}
}

func (d *Discoverer) Discover(ctx context.Context, rawLink string, auto bool) (Result, error) {
	if !auto {
		return fallbackResult(), nil
	}
	d.mu.Lock()
	if c, ok := d.cache[rawLink]; ok && time.Now().Before(c.ExpiresAt) {
		d.mu.Unlock()
		return c.Result, nil
	}
	d.mu.Unlock()

	provider, id, err := ParseLink(rawLink)
	if err != nil {
		return fallbackResult(), err
	}

	var res Result
	switch provider {
	case ProviderVK:
		res, err = discoverVK(ctx, id)
	case ProviderYandex:
		res, err = discoverYandex(ctx, id)
	}
	if err != nil || len(res.Servers) == 0 {
		return fallbackResult(), err
	}

	d.mu.Lock()
	d.cache[rawLink] = cacheEntry{ExpiresAt: time.Now().Add(30 * time.Minute), Result: res}
	d.mu.Unlock()

	return res, nil
}

func fallbackResult() Result {
	servers := make([]Server, 0, len(fallbackTURNHosts))
	for _, h := range fallbackTURNHosts {
		servers = append(servers, Server{Host: h, Port: "3478"})
	}
	return Result{Provider: ProviderVK, Servers: servers}
}

func discoverVK(_ context.Context, link string) (Result, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	doRequest := func(data string, endpoint string) (map[string]any, error) {
		req, err := http.NewRequest("POST", endpoint, bytes.NewBufferString(data))
		if err != nil {
			return nil, err
		}
		req.Header.Add("User-Agent", "Mozilla/5.0")
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if err = json.Unmarshal(body, &out); err != nil {
			return nil, err
		}
		return out, nil
	}

	resp, err := doRequest("client_secret=QbYic1K3lEV5kTGiqlq2&client_id=6287487&scopes=audio_anonymous%2Cvideo_anonymous%2Cphotos_anonymous%2Cprofile_anonymous&isApiOauthAnonymEnabled=false&version=1&app_id=6287487", "https://login.vk.ru/?act=get_anonym_token")
	if err != nil {
		return Result{}, err
	}
	token1, _ := resp["data"].(map[string]any)["access_token"].(string)

	resp, err = doRequest("access_token="+token1, "https://api.vk.ru/method/calls.getAnonymousAccessTokenPayload?v=5.264&client_id=6287487")
	if err != nil {
		return Result{}, err
	}
	token2, _ := resp["response"].(map[string]any)["payload"].(string)

	resp, err = doRequest(fmt.Sprintf("client_id=6287487&token_type=messages&payload=%s&client_secret=QbYic1K3lEV5kTGiqlq2&version=1&app_id=6287487", token2), "https://login.vk.ru/?act=get_anonym_token")
	if err != nil {
		return Result{}, err
	}
	token3, _ := resp["data"].(map[string]any)["access_token"].(string)

	resp, err = doRequest(fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=123&access_token=%s", link, token3), "https://api.vk.ru/method/calls.getAnonymousToken?v=5.264")
	if err != nil {
		return Result{}, err
	}
	token4, _ := resp["response"].(map[string]any)["token"].(string)

	resp, err = doRequest("session_data=%7B%22version%22%3A2%2C%22device_id%22%3A%22"+uuid.NewString()+"%22%2C%22client_version%22%3A1.1%2C%22client_type%22%3A%22SDK_JS%22%7D&method=auth.anonymLogin&format=JSON&application_key=CGMMEJLGDIHBABABA", "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return Result{}, err
	}
	token5, _ := resp["session_key"].(string)

	resp, err = doRequest(fmt.Sprintf("joinLink=%s&isVideo=false&protocolVersion=5&anonymToken=%s&method=vchat.joinConversationByLink&format=JSON&application_key=CGMMEJLGDIHBABABA&session_key=%s", link, token4, token5), "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return Result{}, err
	}
	turnServer, ok := resp["turn_server"].(map[string]any)
	if !ok {
		return Result{}, errors.New("turn_server missing")
	}
	user, _ := turnServer["username"].(string)
	pass, _ := turnServer["credential"].(string)
	urls, _ := turnServer["urls"].([]any)
	servers := make([]Server, 0, len(urls))
	for _, raw := range urls {
		u, _ := raw.(string)
		if !strings.HasPrefix(u, "turn:") && !strings.HasPrefix(u, "turns:") {
			continue
		}
		clean := strings.Split(u, "?")[0]
		host, port, _ := netHostPort(strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:"))
		servers = append(servers, Server{Host: host, Port: port, Username: user, Password: pass})
	}
	if len(servers) == 0 {
		return Result{}, errors.New("no turn servers")
	}
	return Result{Provider: ProviderVK, Servers: dedupe(servers)}, nil
}

func discoverYandex(ctx context.Context, link string) (Result, error) {
	type confResponse struct {
		RoomID              string `json:"room_id"`
		PeerID              string `json:"peer_id"`
		Credentials         string `json:"credentials"`
		ClientConfiguration struct {
			MediaServerURL string `json:"media_server_url"`
		} `json:"client_configuration"`
	}
	type helloReq struct {
		UID   string `json:"uid"`
		Hello struct {
			ParticipantID string `json:"participantId"`
			RoomID        string `json:"roomId"`
			ServiceName   string `json:"serviceName"`
			Credentials   string `json:"credentials"`
			SendAudio     bool   `json:"sendAudio"`
			SendVideo     bool   `json:"sendVideo"`
		} `json:"hello"`
	}
	type wsResp struct {
		ServerHello struct {
			RtcConfiguration struct {
				IceServers []struct {
					Urls       []string `json:"urls"`
					Username   string   `json:"username"`
					Credential string   `json:"credential"`
				} `json:"iceServers"`
			} `json:"rtcConfiguration"`
		} `json:"serverHello"`
	}

	endpoint := fmt.Sprintf("https://cloud-api.yandex.ru/telemost_front/v2/telemost/conferences/https%%3A%%2F%%2Ftelemost.yandex.ru%%2Fj%%2F%s/connection?next_gen_media_platform_allowed=false", link)
	req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	var conf confResponse
	if err = json.NewDecoder(resp.Body).Decode(&conf); err != nil {
		return Result{}, err
	}
	ws, _, err := websocket.DefaultDialer.Dial(conf.ClientConfiguration.MediaServerURL, http.Header{"Origin": []string{"https://telemost.yandex.ru"}})
	if err != nil {
		return Result{}, err
	}
	defer ws.Close()
	msg := helloReq{UID: uuid.NewString()}
	msg.Hello.ParticipantID = conf.PeerID
	msg.Hello.RoomID = conf.RoomID
	msg.Hello.ServiceName = "telemost"
	msg.Hello.Credentials = conf.Credentials
	if err = ws.WriteJSON(msg); err != nil {
		return Result{}, err
	}
	for {
		_, b, err := ws.ReadMessage()
		if err != nil {
			return Result{}, err
		}
		var out wsResp
		if err := json.Unmarshal(b, &out); err != nil {
			continue
		}
		servers := make([]Server, 0)
		for _, ice := range out.ServerHello.RtcConfiguration.IceServers {
			for _, u := range ice.Urls {
				if !strings.HasPrefix(u, "turn:") && !strings.HasPrefix(u, "turns:") {
					continue
				}
				clean := strings.Split(u, "?")[0]
				h, p, _ := netHostPort(strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:"))
				servers = append(servers, Server{Host: h, Port: p, Username: ice.Username, Password: ice.Credential})
			}
		}
		if len(servers) > 0 {
			return Result{Provider: ProviderYandex, Servers: dedupe(servers)}, nil
		}
	}
}

func netHostPort(addr string) (string, string, error) {
	host, port, ok := strings.Cut(addr, ":")
	if !ok || host == "" {
		return addr, "3478", nil
	}
	return host, port, nil
}

func dedupe(in []Server) []Server {
	seen := map[string]struct{}{}
	out := make([]Server, 0, len(in))
	for _, s := range in {
		k := s.Host + ":" + s.Port
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	return out
}
