// Package llmx 收口大模型生成调用：openai（/chat/completions 兼容）、
// anthropic（/v1/messages 兼容）、ollama（本地，OpenAI 兼容协议）与
// builtin（内置托管模型，经 chatsc 状态文件发现 sidecar 端口后按 openai
// 协议走）四种，供条目优化等场景使用。
package llmx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"okryptos/internal/chatsc"
	"okryptos/internal/config"
)

// errBuiltinStarting 内置模型未就绪时的统一错误：want 已写，daemon 会拉起
// sidecar，调用方直接透传给用户即可。
var errBuiltinStarting = errors.New("内置模型启动中（已请求拉起 sidecar），请稍后重试")

// Client 单个 profile 的生成客户端。
type Client struct {
	p       config.LLMProfile
	timeout time.Duration
	hc      *http.Client
	// notReady 非 nil 时（builtin 档 sidecar 未就绪）Chat/Test 立即返回该错误。
	notReady error
}

// New 构造客户端；timeout<=0 钳 30s（生成比 embed 慢，沿用 embedx 的
// 零值钳制教训但阈值不同）。kind 不做校验（任何 kind 都返回非 nil 客户端），
// 非法 kind 在 Chat 的 default 分支报错（调用方校验兜底）。
// kind=builtin 时从 chatsc 状态文件发现端口：healthy 且 model_id 与
// profile.Model 一致 → 按 openai 协议走（healthy 即视为一次使用，Touch 更新
// last_used 防 daemon 空闲回收——embedx 是在成功调用后 Touch，此处简化为
// New 内 Touch 一次）；未就绪或模型身份不匹配 → RequestStart 写 want 请求
// 拉起，client 标记 notReady。契约不变：永不返回 nil。
func New(p config.LLMProfile, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	var notReady error
	if p.Kind == "builtin" {
		// healthy 之外还要校验模型身份：sidecar 跑着的模型与 profile.Model
		// 不同（如两槽各挂不同 builtin，janitor 只维持过滤槽那个）时不能
		// 直接对话——那是"跑着 A 模型却按 B profile 对话"的静默错配；
		// 走 notReady 分支，RequestStart 让 daemon 换型重拉。
		if st := chatsc.LoadState(); st != nil && st.Healthy() && st.ModelID == p.Model {
			p.BaseURL = st.BaseURL()
			p.Kind = "openai"
			chatsc.Touch()
		} else {
			chatsc.RequestStart()
			notReady = errBuiltinStarting
		}
	}
	if p.Kind == "ollama" {
		p.BaseURL = normalizeOllamaBase(p.BaseURL)
	}
	if p.Kind == "anthropic" {
		// 用户按 openai 习惯把 base_url 填成 …/v1 时先去重，否则 endpoint 拼出
		// …/v1/v1/messages 404（embedx 已为 ollama 修过同款）。
		p.BaseURL = strings.TrimSuffix(strings.TrimRight(p.BaseURL, "/"), "/v1")
	}
	return &Client{p: p, timeout: timeout, hc: &http.Client{Timeout: timeout}, notReady: notReady}
}

// Usage token 消耗：openai 的 prompt/completion_tokens 与 anthropic 的
// input/output_tokens 归一到这两个字段；服务不回 usage 时为零值。
type Usage struct {
	Prompt     int `json:"prompt"`
	Completion int `json:"completion"`
}

// Reply 一次生成的完整结果：正文 + 思考过程 + token 消耗。
type Reply struct {
	Text      string
	Reasoning string // 思考过程（reasoning_content / thinking 块），无则空
	// Truncated 输出触顶 max_tokens 被服务端截断（openai finish_reason=length /
	// anthropic stop_reason=max_tokens 归一）。截断时 Text 多为半截，调用方
	// 应报明确的截断错误而不是笼统的解析失败。
	Truncated bool
	Usage     Usage
}

// Chat 单轮非流式对话：system + user 进，Reply 出。
// profile.MaxTokens>0 时覆盖调用方 maxTokens（用户显式配置优先）。
func (c *Client) Chat(ctx context.Context, system, user string, maxTokens int) (Reply, error) {
	if c.notReady != nil {
		return Reply{}, c.notReady
	}
	if c.p.MaxTokens > 0 {
		maxTokens = c.p.MaxTokens
	}
	switch c.p.Kind {
	case "openai", "ollama":
		return c.chatOpenAI(ctx, system, user, maxTokens)
	case "anthropic":
		return c.chatAnthropic(ctx, system, user, maxTokens)
	default:
		return Reply{}, fmt.Errorf("未知 llm 类型: %q（openai|anthropic|ollama|builtin）", c.p.Kind)
	}
}

// Test 连通性检查：发一条极短请求验证地址/鉴权/模型名。
func (c *Client) Test(ctx context.Context) error {
	_, err := c.Chat(ctx, "ping", "ping", 1)
	return err
}

func (c *Client) endpoint(path string) string {
	return strings.TrimRight(c.p.BaseURL, "/") + path
}

// applyTemperature profile.Temperature 非空时解析并写入请求体；非法值报错
// （GUI 保存时已校验，此处兜住手改 config.toml 的情况）。
func (c *Client) applyTemperature(body map[string]any) error {
	if strings.TrimSpace(c.p.Temperature) == "" {
		return nil
	}
	t, err := strconv.ParseFloat(strings.TrimSpace(c.p.Temperature), 64)
	if err != nil {
		return fmt.Errorf("temperature 配置非法: %q", c.p.Temperature)
	}
	body["temperature"] = t
	return nil
}

func (c *Client) doJSON(ctx context.Context, url string, headers map[string]string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		snip := string(raw)
		if len(snip) > 300 {
			snip = snip[:300] + "…"
		}
		return fmt.Errorf("%s %d: %s", c.p.Kind, resp.StatusCode, snip)
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) chatOpenAI(ctx context.Context, system, user string, maxTokens int) (Reply, error) {
	body := map[string]any{
		"model": c.p.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"max_tokens": maxTokens,
		// temperature 缺省不传（服务端默认值兼容性最好，如 Kimi k3 锁死=1）；
		// 用户在高级参数里显式配置时才带上。
	}
	if err := c.applyTemperature(body); err != nil {
		return Reply{}, err
	}
	var out struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"` // stop/length/…；length=触顶截断
			Message      struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"` // 思考过程（DeepSeek/Kimi 等推理模型）
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := c.doJSON(ctx, c.endpoint("/chat/completions"),
		map[string]string{"Authorization": "Bearer " + c.p.APIKey}, body, &out); err != nil {
		return Reply{}, err
	}
	if len(out.Choices) == 0 {
		return Reply{}, fmt.Errorf("openai 响应无 choices")
	}
	return Reply{
		Text:      out.Choices[0].Message.Content,
		Reasoning: out.Choices[0].Message.ReasoningContent,
		Truncated: out.Choices[0].FinishReason == "length",
		Usage:     Usage{Prompt: out.Usage.Prompt, Completion: out.Usage.Completion},
	}, nil
}

func (c *Client) chatAnthropic(ctx context.Context, system, user string, maxTokens int) (Reply, error) {
	body := map[string]any{
		"model":      c.p.Model,
		"system":     system,
		"messages":   []map[string]string{{"role": "user", "content": user}},
		"max_tokens": maxTokens,
	}
	if err := c.applyTemperature(body); err != nil {
		return Reply{}, err
	}
	var out struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"` // 扩展思考块（thinking 开启时）
		} `json:"content"`
		StopReason string `json:"stop_reason"` // end_turn/max_tokens/…；max_tokens=触顶截断
		Usage      struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := c.doJSON(ctx, c.endpoint("/v1/messages"), map[string]string{
		"x-api-key":         c.p.APIKey,
		"anthropic-version": "2023-06-01",
	}, body, &out); err != nil {
		return Reply{}, err
	}
	var text, reasoning string
	for _, b := range out.Content {
		switch b.Type {
		case "thinking":
			reasoning += b.Thinking
		default: // text 及未标注类型的块
			if b.Text != "" {
				text += b.Text
			}
		}
	}
	if text == "" && len(out.Content) == 0 {
		return Reply{}, fmt.Errorf("anthropic 响应无 content")
	}
	return Reply{
		Text:      text,
		Reasoning: reasoning,
		Truncated: out.StopReason == "max_tokens",
		Usage:     Usage{Prompt: out.Usage.Input, Completion: out.Usage.Output},
	}, nil
}

// normalizeOllamaBase ollama 的 OpenAI 兼容端点挂在 /v1 下：留空按本机默认，
// 缺 /v1 后缀自动补上（用户按习惯填 http://host:11434 会 404，embedx 修过同款）。
func normalizeOllamaBase(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "http://localhost:11434"
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base
}
