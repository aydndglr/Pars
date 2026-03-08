// internal/skills/manager.go
// 🚀 DÜZELTME V2: Kangal Tool Desteği Eklendi + Thread-Safety İyileştirildi
// ⚠️ DİKKAT: Kangal araçları (kangal_control) bu manager üzerinden register edilecek

package skills

import (
	"fmt"
	"sort"
	"sync"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// ========================================================================
// 📦 MANAGER YAPISI
// ========================================================================
// Manager: Tüm yeteneklerin (Tools) dinamik kayıt defteri.
// Thread-safe yapı ile eşzamanlı okuma/yazma işlemlerini güvenle yönetir.
type Manager struct {
	tools map[string]kernel.Tool
	mu    sync.RWMutex
}

// ========================================================================
// 🆕 YENİ: Manager Oluşturucu
// ========================================================================
// NewManager: Boş ve thread-safe bir yönetici oluşturur.
func NewManager() *Manager {
	return &Manager{
		tools: make(map[string]kernel.Tool),
	}
}

// ========================================================================
// 📝 TOOL KAYIT İŞLEMLERİ
// ========================================================================
// Register: Sisteme yeni bir araç ekler.
// Eğer araç zaten varsa üzerine yazar (Update) ve 'true' döner. Yeni eklendiyse 'false' döner.
func (m *Manager) Register(t kernel.Tool) bool {
	// 🚨 DÜZELTME #1: Nil kontrolü
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
	
	// 🚨 DÜZELTME #2: Tool name validation
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

// RegisterMultiple: Birden fazla aracı tek seferde kaydeder (Kangal init için kullanışlı)
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

// Unregister: Verilen isimdeki aracı sistemden (hafızadan) siler.
// Dinamik yeteneklerin çalışma anında (runtime) sökülebilmesi için şarttır.
func (m *Manager) Unregister(name string) bool {
	// 🚨 DÜZELTME #3: Nil kontrolü
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

// ========================================================================
// 🔍 TOOL SORGULAMA İŞLEMLERİ
// ========================================================================
// HasTool: Aracın sistemde kayıtlı olup olmadığını hızlıca kontrol eder.
func (m *Manager) HasTool(name string) bool {
	// 🚨 DÜZELTME #4: Nil kontrolü
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

// GetTool: İsmi verilen aracı bulur ve döndürür.
func (m *Manager) GetTool(name string) (kernel.Tool, error) {
	// 🚨 DÜZELTME #5: Nil kontrolü
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

	// 🚨 DÜZELTME #6: Daha açıklayıcı hata mesajı
	return nil, fmt.Errorf("tool bulunamadı: %s (kayıtlı tool sayısı: %d)", name, len(m.tools))
}

// ListTools: LLM'e göndermek için araç listesini hazırlar.
// 🚨 DİKKAT: Kangal araçları da bu listeye dahil edilir.
func (m *Manager) ListTools() []kernel.Tool {
	// 🚨 DÜZELTME #7: Nil kontrolü
	if m == nil {
		return []kernel.Tool{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// 🚨 DÜZELTME #8: Başlangıç kapasitesi ayarla (performans)
	list := make([]kernel.Tool, 0, len(m.tools))
	for _, t := range m.tools {
		// 🚨 DÜZELTME #9: Nil tool'ları listeye ekleme
		if t != nil {
			list = append(list, t)
		}
	}

	// İsim sırasına göre dizelim ki LLM her seferinde aynı sırayla görüp halüsinasyon görmesin
	sort.Slice(list, func(i, j int) bool {
		// 🚨 DÜZELTME #10: Nil tool name çağrısı panic önleme
		if list[i] == nil || list[j] == nil {
			return list[i] != nil
		}
		return list[i].Name() < list[j].Name()
	})

	return list
}

// GetToolNames: Sadece tool isimlerini döndür (hızlı lookup için)
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
	return names
}

// ========================================================================
// 📊 İSTATİSTİK VE DEBUG İŞLEMLERİ
// ========================================================================
// Count: Sistemdeki aktif araç sayısını döner.
func (m *Manager) Count() int {
	// 🚨 DÜZELTME #11: Nil kontrolü
	if m == nil {
		return 0
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tools)
}

// GetAllTools: 🚀 YENİ - Tüm tool'ları map olarak döndür (debug için)
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
	return result
}

// GetStats: Tool istatistiklerini döndür (debug/telemetry için)
func (m *Manager) GetStats() map[string]interface{} {
	if m == nil {
		return map[string]interface{}{"error": "manager nil"}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Tool tiplerine göre sayım
	nativeCount := 0
	pythonCount := 0
	kangalCount := 0
	otherCount := 0

	for name := range m.tools {
		switch {
		case name == "kangal_control":
			kangalCount++
		case name == "dev_studio", name == "edit_python_tool", name == "delete_python_tool":
			nativeCount++
		case name == "ai_code_editor":
			nativeCount++
		default:
			otherCount++
		}
	}

	return map[string]interface{}{
		"total_tools":    len(m.tools),
		"native_tools":   nativeCount,
		"python_tools":   pythonCount,
		"kangal_tools":   kangalCount,
		"other_tools":    otherCount,
		"tool_names":     m.GetToolNames(),
		"timestamp":      time.Now().Format("15:04:05"),
	}
}

// Clear: 🚀 YENİ - Tüm tool'ları temizle (reset için)
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

// ========================================================================
// 🐕 KANGAL TOOL YÖNETİMİ (ÖZEL FONKSİYONLAR)
// ========================================================================
// HasKangalTool: Kangal kontrol aracı kayıtlı mı kontrol et
func (m *Manager) HasKangalTool() bool {
	return m.HasTool("kangal_control")
}

// GetKangalTool: Kangal kontrol aracını döndür
func (m *Manager) GetKangalTool() (kernel.Tool, error) {
	return m.GetTool("kangal_control")
}

// IsToolActive: Tool aktif mi (nil değil ve kayıtlı) kontrol et
func (m *Manager) IsToolActive(name string) bool {
	if m == nil {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	tool, exists := m.tools[name]
	return exists && tool != nil
}

// ========================================================================
// 🆕 YENİ: TOOL VALIDATION HELPER'LARI
// ========================================================================
// ValidateToolName: Tool ismi geçerli mi kontrol et
func ValidateToolName(name string) bool {
	if name == "" {
		return false
	}

	// İzin verilen karakterler: a-z, A-Z, 0-9, _, -
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
			return false
		}
	}

	return true
}

// GetToolByCategory: Kategoriye göre tool'ları filtrele (örn: "kangal", "filesystem", "network")
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

		// Kategori eşleşmesi (basit string prefix check)
		if category == "" || strings.HasPrefix(name, category) {
			result = append(result, tool)
		}
	}

	return result
}

// ========================================================================
// 🧹 CLEANUP VE MAINTENANCE
// ========================================================================
// RemoveNilTools: Nil tool'ları temizle (memory leak önleme)
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

// ExportTools: Tool listesini JSON-export için hazırla (debug/backup için)
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

	return export
}