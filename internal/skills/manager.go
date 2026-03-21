package skills

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

type Manager struct {
	tools map[string]kernel.Tool
	mu    sync.RWMutex
}

func NewManager() *Manager {
	logger.Debug("📦 [Manager] Yeni manager oluşturuldu")
	return &Manager{
		tools: make(map[string]kernel.Tool),
	}
}

func (m *Manager) Register(t kernel.Tool) bool {
	if m == nil {
		logger.Error("❌ [Manager] Register: Manager nil!")
		return false
	}

	if t == nil {
		logger.Warn("⚠️ [Manager] Register: Nil tool kayıt edilemez.")
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	toolName := t.Name()

	if toolName == "" {
		logger.Warn("⚠️ [Manager] Register: İsimsiz tool kayıt edilemez.")
		return false
	}

	_, isUpdate := m.tools[toolName]
	m.tools[toolName] = t

	if isUpdate {
		logger.Debug("🔄 [Manager] Tool güncellendi: %s", toolName)
	} else {
		logger.Debug("✅ [Manager] Tool kaydedildi: %s", toolName)
	}

	return isUpdate
}

func (m *Manager) RegisterMultiple(tools []kernel.Tool) int {
	if m == nil {
		logger.Error("❌ [Manager] RegisterMultiple: Manager nil!")
		return 0
	}

	count := 0
	for _, t := range tools {
		if t != nil {
			m.Register(t)
			count++
		}
	}

	logger.Debug("📦 [Manager] %d tool toplu olarak kaydedildi", count)
	return count
}

func (m *Manager) Unregister(name string) bool {
	if m == nil {
		logger.Error("❌ [Manager] Unregister: Manager nil!")
		return false
	}

	if name == "" {
		logger.Warn("⚠️ [Manager] Unregister: Boş isim silinemez.")
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tools[name]; exists {
		delete(m.tools, name)
		logger.Debug("🗑️ [Manager] Tool silindi: %s", name)
		return true
	}

	logger.Debug("⚠️ [Manager] Tool zaten yoktu: %s", name)
	return false
}

func (m *Manager) HasTool(name string) bool {
	if m == nil {
		return false
	}

	if name == "" {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.tools[name]
	return exists
}

func (m *Manager) GetTool(name string) (kernel.Tool, error) {
	if m == nil {
		return nil, fmt.Errorf("manager nil")
	}

	if name == "" {
		return nil, fmt.Errorf("tool adı boş olamaz")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if t, exists := m.tools[name]; exists {
		return t, nil
	}

	return nil, fmt.Errorf("tool bulunamadı: %s (kayıtlı tool sayısı: %d)", name, len(m.tools))
}

func (m *Manager) ListTools() []kernel.Tool {
	if m == nil {
		return []kernel.Tool{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]kernel.Tool, 0, len(m.tools))
	for _, t := range m.tools {
		if t != nil {
			list = append(list, t)
		}
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i] == nil || list[j] == nil {
			return list[i] != nil
		}
		return list[i].Name() < list[j].Name()
	})

	logger.Debug("📋 [Manager] %d tool listelendi", len(list))
	return list
}

func (m *Manager) GetToolNames() []string {
	if m == nil {
		return []string{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.tools))
	for name := range m.tools {
		names = append(names, name)
	}

	sort.Strings(names)
	logger.Debug("📋 [Manager] %d tool ismi döndürüldü", len(names))
	return names
}

func (m *Manager) Count() int {
	if m == nil {
		return 0
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tools)
}

func (m *Manager) GetAllTools() map[string]kernel.Tool {
	if m == nil {
		return make(map[string]kernel.Tool)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]kernel.Tool, len(m.tools))
	for k, v := range m.tools {
		result[k] = v
	}
	logger.Debug("📋 [Manager] Tüm tool'lar döndürüldü: %d adet", len(result))
	return result
}

func (m *Manager) GetStats() map[string]interface{} {
	if m == nil {
		return map[string]interface{}{"error": "manager nil"}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	nativeCount := 0
	pythonCount := 0
	kangalCount := 0
	otherCount := 0

	toolNames := make([]string, 0, len(m.tools))

	for name, tool := range m.tools {
		toolNames = append(toolNames, name)

		if name == "kangal_control" {
			kangalCount++
		} else if _, isPython := tool.(*PythonTool); isPython {
			pythonCount++
		} else if strings.HasPrefix(name, "fs_") || strings.HasPrefix(name, "sys_") || name == "ai_code_editor" || name == "dev_studio" {
			nativeCount++
		} else {
			otherCount++
		}
	}

	sort.Strings(toolNames)

	stats := map[string]interface{}{
		"total_tools":    len(m.tools),
		"native_tools":   nativeCount,
		"python_tools":   pythonCount,
		"kangal_tools":   kangalCount,
		"other_tools":    otherCount,
		"tool_names":     toolNames,
		"timestamp":      time.Now().Format("15:04:05"),
	}

	logger.Debug("📊 [Manager] İstatistikler: Toplam=%d, Native=%d, Python=%d, Kangal=%d",
		len(m.tools), nativeCount, pythonCount, kangalCount)
	return stats
}

func (m *Manager) Clear() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	count := len(m.tools)
	m.tools = make(map[string]kernel.Tool)
	logger.Debug("🧹 [Manager] Tüm tool'lar temizlendi: %d adet", count)
}

func (m *Manager) HasKangalTool() bool {
	hasTool := m.HasTool("kangal_control")
	logger.Debug("🐕 [Manager] Kangal tool kontrolü: %v", hasTool)
	return hasTool
}

func (m *Manager) GetKangalTool() (kernel.Tool, error) {
	tool, err := m.GetTool("kangal_control")
	if err == nil {
		logger.Debug("🐕 [Manager] Kangal tool bulundu")
	} else {
		logger.Debug("⚠️ [Manager] Kangal tool bulunamadı: %v", err)
	}
	return tool, err
}

func (m *Manager) IsToolActive(name string) bool {
	if m == nil {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	tool, exists := m.tools[name]
	active := exists && tool != nil
	logger.Debug("🔍 [Manager] Tool aktiflik kontrolü: %s = %v", name, active)
	return active
}

func ValidateToolName(name string) bool {
	if name == "" {
		return false
	}

	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	for _, c := range name {
		found := false
		for _, vc := range validChars {
			if c == vc {
				found = true
				break
			}
		}
		if !found {
			logger.Debug("⚠️ [Manager] Geçersiz tool karakteri: %c", c)
			return false
		}
	}

	return true
}

func (m *Manager) GetToolByCategory(category string) []kernel.Tool {
	if m == nil {
		return []kernel.Tool{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []kernel.Tool
	for name, tool := range m.tools {
		if tool == nil {
			continue
		}

		if category == "" || strings.HasPrefix(name, category) {
			result = append(result, tool)
		}
	}

	logger.Debug("📋 [Manager] Kategoriye göre %d tool bulundu: %s", len(result), category)
	return result
}

func (m *Manager) RemoveNilTools() int {
	if m == nil {
		return 0
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	removed := 0
	for name, tool := range m.tools {
		if tool == nil {
			delete(m.tools, name)
			removed++
			logger.Debug("🧹 [Manager] Nil tool temizlendi: %s", name)
		}
	}

	if removed > 0 {
		logger.Info("🧹 [Manager] %d nil tool temizlendi", removed)
	}

	return removed
}

func (m *Manager) ExportTools() []map[string]interface{} {
	if m == nil {
		return []map[string]interface{}{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var export []map[string]interface{}
	for name, tool := range m.tools {
		if tool == nil {
			continue
		}

		export = append(export, map[string]interface{}{
			"name":        name,
			"description": tool.Description(),
			"parameters":  tool.Parameters(),
		})
	}

	logger.Debug("📤 [Manager] %d tool export edildi", len(export))
	return export
}