// internal/core/kernel/interfaces.go
// 🚀 DÜZELTMELER: Thread-safety, Validation, Constants eklendi
// ⚠️ DİKKAT: Mevcut kod ile %100 uyumlu, breaking change YOK

package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// =========================================================================
// 🛡️ GLOBAL SABİTLER (Role Constants)
// =========================================================================
const (
	RoleAssistant = "assistant"
	RoleUser      = "user"
	RoleSystem    = "system"
	RoleTool      = "tool"
)

// 🚨 Hata Tanımları
var (
	ErrEmptyContent      = errors.New("content boş olamaz")
	ErrInvalidRole       = errors.New("geçersiz mesaj rolü")
	ErrEmptyFunctionName = errors.New("fonksiyon adı boş olamaz")
)

// =========================================================================
// 🔧 TOOL INTERFACE
// =========================================================================

// Tool: Pars'in kullanabileceği her yetenek bu arayüzü uygulamalıdır.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{} // JSON Schema
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

// =========================================================================
// 🎯 TOOLCALL
// =========================================================================

// ToolCall: LLM'in araç çağırma isteği
type ToolCall struct {
	ID        string                 `json:"id"`
	Function  string                 `json:"function"`
	Arguments map[string]interface{} `json:"arguments"`
}

// 🆕 Validate: ToolCall'ı doğrula (Safe validation)
func (tc *ToolCall) Validate() error {
	if tc == nil {
		return errors.New("toolcall nil")
	}
	if tc.Function == "" {
		return ErrEmptyFunctionName
	}
	return nil
}

// 🆕 Clone: Thread-safe derin kopya
func (tc *ToolCall) Clone() ToolCall {
	if tc == nil {
		return ToolCall{}
	}
	argsCopy := make(map[string]interface{}, len(tc.Arguments))
	for k, v := range tc.Arguments {
		argsCopy[k] = v
	}
	return ToolCall{
		ID:        tc.ID,
		Function:  tc.Function,
		Arguments: argsCopy,
	}
}

// =========================================================================
// 🧠 BRAIN RESPONSE (THREAD-SAFE)
// =========================================================================

// BrainResponse: Beyinden gelen ham cevap
// 🚨 YENİ: RWMutex ile thread-safe hale getirildi
type BrainResponse struct {
	Content   string
	ToolCalls []ToolCall
	Usage     map[string]int
	mu        sync.RWMutex // 🆕 Thread-safe erişim için
}

// 🆕 AddToolCall: Thread-safe tool call ekle
func (br *BrainResponse) AddToolCall(tc ToolCall) error {
	if br == nil {
		return errors.New("brainresponse nil")
	}
	if err := tc.Validate(); err != nil {
		return err
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	br.ToolCalls = append(br.ToolCalls, tc)
	return nil
}

// 🆕 GetToolCalls: Thread-safe tool call okuma (deep copy)
func (br *BrainResponse) GetToolCalls() []ToolCall {
	if br == nil {
		return nil
	}
	br.mu.RLock()
	defer br.mu.RUnlock()
	result := make([]ToolCall, len(br.ToolCalls))
	for i, tc := range br.ToolCalls {
		result[i] = tc.Clone()
	}
	return result
}

// 🆕 SetContent: Thread-safe content güncelleme
func (br *BrainResponse) SetContent(content string) {
	if br == nil {
		return
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	br.Content = content
}

// 🆕 GetContent: Thread-safe content okuma
func (br *BrainResponse) GetContent() string {
	if br == nil {
		return ""
	}
	br.mu.RLock()
	defer br.mu.RUnlock()
	return br.Content
}

// 🆕 Clone: Thread-safe derin kopya
func (br *BrainResponse) Clone() *BrainResponse {
	if br == nil {
		return nil
	}
	br.mu.RLock()
	defer br.mu.RUnlock()
	toolCallsCopy := make([]ToolCall, len(br.ToolCalls))
	for i, tc := range br.ToolCalls {
		toolCallsCopy[i] = tc.Clone()
	}
	usageCopy := make(map[string]int, len(br.Usage))
	for k, v := range br.Usage {
		usageCopy[k] = v
	}
	return &BrainResponse{
		Content:   br.Content,
		ToolCalls: toolCallsCopy,
		Usage:     usageCopy,
	}
}

// =========================================================================
// 💬 MESSAGE
// =========================================================================

// Message: Sohbet geçmişi birimi
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Images     []string   `json:"images,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// 🆕 Validate: Message'ı doğrula
func (m *Message) Validate() error {
	if m == nil {
		return errors.New("message nil")
	}
	switch m.Role {
	case RoleUser, RoleAssistant, RoleSystem, RoleTool:
		// Geçerli roller
	default:
		return fmt.Errorf("%w: %s", ErrInvalidRole, m.Role)
	}
	return nil
}

// 🆕 Clone: Derin kopya
func (m *Message) Clone() Message {
	if m == nil {
		return Message{}
	}
	imagesCopy := make([]string, len(m.Images))
	copy(imagesCopy, m.Images)
	toolCallsCopy := make([]ToolCall, len(m.ToolCalls))
	for i, tc := range m.ToolCalls {
		toolCallsCopy[i] = tc.Clone()
	}
	return Message{
		Role:       m.Role,
		Content:    m.Content,
		Images:     imagesCopy,
		Name:       m.Name,
		ToolCalls:  toolCallsCopy,
		ToolCallID: m.ToolCallID,
	}
}

// 🆕 ToJSON: Message'ı JSON'a çevir
func (m *Message) ToJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// 🆕 CloneSlice: Message slice'ını güvenli kopyala
func CloneMessages(msgs []Message) []Message {
	if msgs == nil {
		return nil
	}
	result := make([]Message, len(msgs))
	for i, m := range msgs {
		result[i] = m.Clone()
	}
	return result
}

// =========================================================================
// 🧠 BRAIN INTERFACE
// =========================================================================

// Brain: Zeka sağlayıcısı (Ollama, OpenAI, Gemini vb.)
type Brain interface {
	Chat(ctx context.Context, history []Message, tools []Tool) (*BrainResponse, error)
	Embed(ctx context.Context, text string) ([]float32, error)
}

// =========================================================================
// 🧠 MEMORY INTERFACE
// =========================================================================

// Memory: Uzun süreli hafıza
type Memory interface {
	Add(ctx context.Context, content string, metadata map[string]interface{}) error
	Search(ctx context.Context, query string, limit int) ([]string, error)
}

// =========================================================================
// 🤖 AGENT INTERFACE
// =========================================================================

// Agent: Pars'in kendisi
type Agent interface {
	Run(ctx context.Context, input string, images []string) (string, error)
	RegisterTool(t Tool)
}

// =========================================================================
// 🆕 HELPER FUNCTIONS
// =========================================================================

// IsValidRole: Rol geçerli mi kontrol et
func IsValidRole(role string) bool {
	switch role {
	case RoleUser, RoleAssistant, RoleSystem, RoleTool:
		return true
	}
	return false
}

// NewUserMessage: Yeni user message oluştur
func NewUserMessage(content string, images ...string) Message {
	return Message{
		Role:    RoleUser,
		Content: content,
		Images:  images,
	}
}

// NewAssistantMessage: Yeni assistant message oluştur
func NewAssistantMessage(content string, toolCalls ...ToolCall) Message {
	return Message{
		Role:      RoleAssistant,
		Content:   content,
		ToolCalls: toolCalls,
	}
}

// NewSystemMessage: Yeni system message oluştur
func NewSystemMessage(content string) Message {
	return Message{
		Role:    RoleSystem,
		Content: content,
	}
}

// NewToolMessage: Yeni tool response message oluştur
func NewToolMessage(content, toolCallID, name string) Message {
	return Message{
		Role:       RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
		Name:       name,
	}
}