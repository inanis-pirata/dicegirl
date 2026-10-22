package main

// Minimal QQ official-bot OpenAPI client: app access token exchange/caching,
// gateway URL lookup, and passive group/C2C text replies. Business errors
// arrive with HTTP 200, so every response is checked on its body code.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"dicegirl/internal/apperr"
)

const (
	// IntentGroupAndC2C is GROUP_AND_C2C_EVENT (1<<25): group @ events,
	// C2C messages and the add/remove/reject lifecycle events.
	IntentGroupAndC2C = 1 << 25

	tokenRefreshMargin = 5 * time.Minute
	httpTimeout        = 15 * time.Second
)

// apiClient talks to api.bot.qq.com with an automatically refreshed app
// access token.
type apiClient struct {
	baseURL string
	appID   string
	secret  string
	http    *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

func newAPIClient(cfg *botConfig) *apiClient {
	return &apiClient{
		baseURL: strings.TrimRight(cfg.APIBase, "/"),
		appID:   cfg.AppID,
		secret:  cfg.Secret,
		http:    &http.Client{Timeout: httpTimeout},
	}
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   string `json:"expires_in"` // documented as a string ("7200")
	Code        int    `json:"code"`
	Message     string `json:"message"`
}

// accessToken returns a cached token, refreshing it shortly before expiry.
// Serialized so concurrent events trigger at most one exchange.
func (c *apiClient) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenExp.Add(-tokenRefreshMargin)) {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	// Re-check after acquiring the lock: another goroutine may have
	// refreshed while we waited.
	if c.token != "" && time.Now().Before(c.tokenExp.Add(-tokenRefreshMargin)) {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{
		"appId":        c.appID,
		"clientSecret": c.secret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/app/getAppAccessToken", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	var tr tokenResponse
	if err := c.doJSON(req, &tr, false); err != nil {
		return "", apperr.Wrap("qqbot.accessToken", apperr.Unauthorized,
			"获取 access_token 失败", err)
	}
	if tr.AccessToken == "" {
		return "", apperr.New("qqbot.accessToken", apperr.Unauthorized,
			fmt.Sprintf("获取 access_token 失败：code=%d message=%s", tr.Code, tr.Message))
	}
	ttl := 7200 * time.Second
	if tr.ExpiresIn != "" {
		var secs int
		if _, err := fmt.Sscan(tr.ExpiresIn, &secs); err == nil && secs > 0 {
			ttl = time.Duration(secs) * time.Second
		}
	}
	c.token = tr.AccessToken
	c.tokenExp = time.Now().Add(ttl)
	return c.token, nil
}

// doJSON runs req and decodes the JSON body, checking business-level codes.
// When withAuth is false no Authorization header is added (the token
// endpoint itself).
func (c *apiClient) doJSON(req *http.Request, out any, withAuth bool) error {
	if withAuth {
		token, err := c.accessToken(req.Context())
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "QQBot "+token)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return apperr.New("qqbot.api", apperr.RateLimited,
			"平台返回 429 频率限制")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return apperr.New("qqbot.api", apperr.UpstreamFailed,
			fmt.Sprintf("平台返回 HTTP %d：%s", resp.StatusCode, truncate(string(raw), 300)))
	}
	if len(raw) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return apperr.Wrap("qqbot.api", apperr.UpstreamFailed,
			"无法解析平台响应", err)
	}
	return nil
}

// apiError is embedded in response structs that carry the standard error
// fields. Code 0 (or a missing code on reply responses) means success.
type apiError struct {
	Code    int    `json:"code"`
	ErrCode int    `json:"err_code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

func (e apiError) failed() bool { return e.Code != 0 || e.ErrCode != 0 }

func (e apiError) Error() string {
	return fmt.Sprintf("平台错误 code=%d err_code=%d message=%s trace=%s",
		e.Code, e.ErrCode, e.Message, e.TraceID)
}

// gatewayResponse is GET /gateway.
type gatewayResponse struct {
	URL string `json:"url"`
	apiError
}

// GatewayURL fetches the WebSocket endpoint (rate limited to 2 QPM).
func (c *apiClient) GatewayURL(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/gateway", nil)
	if err != nil {
		return "", err
	}
	var gr gatewayResponse
	if err := c.doJSON(req, &gr, true); err != nil {
		return "", err
	}
	if gr.failed() {
		return "", apperr.New("qqbot.GatewayURL", classifySendError(apiError{
			Code: gr.Code, Message: gr.Message, TraceID: gr.TraceID,
		}), "获取 gateway 失败："+gr.Error())
	}
	if gr.URL == "" {
		return "", apperr.New("qqbot.GatewayURL", apperr.UpstreamFailed, "gateway url 为空")
	}
	return gr.URL, nil
}

// messageRequest is the body of /v2/groups/{id}/messages and
// /v2/users/{id}/messages for a passive reply.
type messageRequest struct {
	Content string       `json:"content"`
	MsgType int          `json:"msg_type"` // 0 = text, 7 = media
	Media   *mediaAttach `json:"media,omitempty"`
	MsgID   string       `json:"msg_id,omitempty"`
	MsgSeq  int          `json:"msg_seq"`
}

// mediaAttach carries the opaque file_info returned by the upload endpoint.
type mediaAttach struct {
	FileInfo string `json:"file_info"`
}

// fileUploadRequest is POST /v2/{groups|users}/{id}/files. file_data is the
// base64 form of the raw bytes (direct uploads are capped at 5 MB).
type fileUploadRequest struct {
	FileType   int    `json:"file_type"` // 4 = file
	FileName   string `json:"file_name"`
	FileData   string `json:"file_data"`
	SrvSendMsg bool   `json:"srv_send_msg"`
}

type fileUploadResponse struct {
	FileInfo string `json:"file_info"`
	FileUUID string `json:"file_uuid"`
	TTL      int    `json:"ttl"`
	ID       string `json:"id"`
	apiError
}

// uploadFile uploads raw bytes as a file and returns the opaque file_info.
// srv_send_msg is false: the file is attached to a later passive reply,
// which does not consume the proactive-message quota.
func (c *apiClient) uploadFile(ctx context.Context, scope, target, fileName string, data []byte) (string, error) {
	if len(data) > 5<<20 {
		return "", apperr.New("qqbot.uploadFile", apperr.LimitExceeded,
			"文件超过 5MB，无法直接上传")
	}
	path := ""
	switch scope {
	case botScopeGroup:
		path = "/v2/groups/" + target + "/files"
	case botScopeC2C:
		path = "/v2/users/" + target + "/files"
	default:
		return "", apperr.New("qqbot.uploadFile", apperr.InvalidArgument, "未知上传范围 "+scope)
	}
	body, err := json.Marshal(fileUploadRequest{
		FileType: 4,
		FileName: fileName,
		FileData: base64.StdEncoding.EncodeToString(data),
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	var fr fileUploadResponse
	if err := c.doJSON(req, &fr, true); err != nil {
		return "", err
	}
	if fr.failed() {
		return "", apperr.Wrap("qqbot.uploadFile", classifySendError(apiError{
			Code: fr.Code, ErrCode: fr.ErrCode, Message: fr.Message, TraceID: fr.TraceID,
		}), "上传文件失败："+fr.Error(), nil)
	}
	if fr.FileInfo == "" {
		return "", apperr.New("qqbot.uploadFile", apperr.UpstreamFailed, "上传响应缺少 file_info")
	}
	return fr.FileInfo, nil
}

// sendFileReply attaches an uploaded file to a passive reply.
func (c *apiClient) sendFileReply(ctx context.Context, scope, target, msgID string, seq int, fileInfo string) error {
	path := ""
	switch scope {
	case botScopeGroup:
		path = "/v2/groups/" + target + "/messages"
	case botScopeC2C:
		path = "/v2/users/" + target + "/messages"
	default:
		return apperr.New("qqbot.sendFileReply", apperr.InvalidArgument, "未知发送范围 "+scope)
	}
	body, err := json.Marshal(messageRequest{
		MsgType: 7,
		Media:   &mediaAttach{FileInfo: fileInfo},
		MsgID:   msgID,
		MsgSeq:  seq,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	var ae apiError
	if err := c.doJSON(req, &ae, true); err != nil {
		return err
	}
	if ae.failed() {
		return apperr.Wrap("qqbot.sendFileReply", classifySendError(ae), ae.Error(), nil)
	}
	return nil
}

// sendReply posts one passive text reply. target is the group openid or the
// user openid; scope selects the path.
func (c *apiClient) sendReply(ctx context.Context, scope, target, msgID, content string, seq int) error {
	path := ""
	switch scope {
	case botScopeGroup:
		path = "/v2/groups/" + target + "/messages"
	case botScopeC2C:
		path = "/v2/users/" + target + "/messages"
	default:
		return apperr.New("qqbot.sendReply", apperr.InvalidArgument, "未知发送范围 "+scope)
	}
	body, _ := json.Marshal(messageRequest{
		Content: content,
		MsgType: 0,
		MsgID:   msgID,
		MsgSeq:  seq,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	var ae apiError
	if err := c.doJSON(req, &ae, true); err != nil {
		return err
	}
	if ae.failed() {
		return apperr.Wrap("qqbot.sendReply", classifySendError(ae), ae.Error(), nil)
	}
	return nil
}

// classifySendError maps documented send error codes onto stable codes so
// callers can decide whether a retry or a quieter failure makes sense.
func classifySendError(e apiError) apperr.Code {
	switch e.Code {
	case 40034100:
		return apperr.RateLimited
	case 40034128, 304103, 40034005:
		return apperr.PassiveWindowExpired
	case 40054005:
		return apperr.RateLimited // dedup collision: bump msg_seq
	case 40054007:
		return apperr.MessageTooLong
	case 40054010:
		return apperr.InvalidArgument // URL not allowed in content
	case 40054004:
		return apperr.NoFriendRelation
	case 40054013:
		return apperr.UserDisabledMessages
	case 40054002:
		return apperr.BotMuted
	default:
		return apperr.UpstreamFailed
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
