package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

const (
	RoleAssistant = "assistant"
	RoleUser      = "user"
	RoleSystem    = "system"
	RoleTool      = "tool"
)

var (
	ErrEmptyContent      = errors.New("content boş olamaz")
	ErrInvalidRole       = errors.New("geçersiz mesaj rolü")
	ErrEmptyFunctionName = errors.New("fonksiyon adı boş olamaz")
)

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{} // JSON Schema
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

type ToolCall struct {
	ID        string                 `json:"id"`
	Function  string                 `json:"function"`
	Arguments map[string]interface{} `json:"arguments"`
}

func (tc *ToolCall) Validate() error {
	if tc == nil {
		return errors.New("toolcall nil")
	}
	if tc.Function == "" {
		return ErrEmptyFunctionName
	}
	return nil
}

func (tc *ToolCall) Clone() ToolCall {
	if tc == nil {
		return ToolCall{}
	}
	

	var argsCopy map[string]interface{}
	if tc.Arguments != nil {
		argsCopy = make(map[string]interface{}, len(tc.Arguments))
		for k, v := range tc.Arguments {
			argsCopy[k] = v
		}
	}
	
	return ToolCall{
		ID:        tc.ID,
		Function:  tc.Function,
		Arguments: argsCopy,
	}
}

type BrainResponse struct {
	Content   string
	ToolCalls []ToolCall
	Usage     map[string]int
	mu        sync.RWMutex 
}

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

func (br *BrainResponse) GetToolCalls() []ToolCall {
	if br == nil {
		return nil
	}
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	if len(br.ToolCalls) == 0 {
		return nil // 🛠️ 
	}
	
	result := make([]ToolCall, len(br.ToolCalls))
	for i, tc := range br.ToolCalls {
		result[i] = tc.Clone()
	}
	return result
}


func (br *BrainResponse) SetContent(content string) {
	if br == nil {
		return
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	br.Content = content
}

func (br *BrainResponse) GetContent() string {
	if br == nil {
		return ""
	}
	br.mu.RLock()
	defer br.mu.RUnlock()
	return br.Content
}

func (br *BrainResponse) Clone() *BrainResponse {
	if br == nil {
		return nil
	}
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	var toolCallsCopy []ToolCall
	if len(br.ToolCalls) > 0 {
		toolCallsCopy = make([]ToolCall, len(br.ToolCalls))
		for i, tc := range br.ToolCalls {
			toolCallsCopy[i] = tc.Clone()
		}
	}
	
	var usageCopy map[string]int
	if br.Usage != nil {
		usageCopy = make(map[string]int, len(br.Usage))
		for k, v := range br.Usage {
			usageCopy[k] = v
		}
	}
	
	return &BrainResponse{
		Content:   br.Content,
		ToolCalls: toolCallsCopy,
		Usage:     usageCopy,
	}
}


type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Images     []string   `json:"images,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

func (m *Message) Validate() error {
	if m == nil {
		return errors.New("message nil")
	}
	switch m.Role {
	case RoleUser, RoleAssistant, RoleSystem, RoleTool:
	default:
		return fmt.Errorf("%w: %s", ErrInvalidRole, m.Role)
	}
	return nil
}

func (m *Message) Clone() Message {
	if m == nil {
		return Message{}
	}
	
	var imagesCopy []string
	if len(m.Images) > 0 {
		imagesCopy = make([]string, len(m.Images))
		copy(imagesCopy, m.Images)
	}
	
	var toolCallsCopy []ToolCall
	if len(m.ToolCalls) > 0 {
		toolCallsCopy = make([]ToolCall, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			toolCallsCopy[i] = tc.Clone()
		}
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

func (m *Message) ToJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

func CloneMessages(msgs []Message) []Message {
	if len(msgs) == 0 {
		return nil
	}
	result := make([]Message, len(msgs))
	for i, m := range msgs {
		result[i] = m.Clone()
	}
	return result
}

type Brain interface {
	Chat(ctx context.Context, history []Message, tools []Tool) (*BrainResponse, error)
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Memory interface {
	Add(ctx context.Context, content string, metadata map[string]interface{}) error
	Search(ctx context.Context, query string, limit int) ([]string, error)
}

type Agent interface {
	Run(ctx context.Context, input string, images []string) (string, error)
	RegisterTool(t Tool)
}


func IsValidRole(role string) bool {
	switch role {
	case RoleUser, RoleAssistant, RoleSystem, RoleTool:
		return true
	}
	return false
}

func NewUserMessage(content string, images ...string) Message {
	if len(images) == 0 {
		images = nil
	}
	return Message{
		Role:    RoleUser,
		Content: content,
		Images:  images,
	}
}

func NewAssistantMessage(content string, toolCalls ...ToolCall) Message {
	if len(toolCalls) == 0 {
		toolCalls = nil
	}
	return Message{
		Role:      RoleAssistant,
		Content:   content,
		ToolCalls: toolCalls,
	}
}

func NewSystemMessage(content string) Message {
	return Message{
		Role:    RoleSystem,
		Content: content,
	}
}

func NewToolMessage(content, toolCallID, name string) Message {
	return Message{
		Role:       RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
		Name:       name,
	}
}