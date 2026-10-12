package prompts

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"temanwa-ai/internal/storage"
)

//go:embed persona_id.md
var defaultPersonaPrompt string

type Manager struct {
	systemPrompt string
}

func NewManager(promptFilePath string) (*Manager, error) {
	prompt := defaultPersonaPrompt
	if promptFilePath != "" {
		if content, err := os.ReadFile(promptFilePath); err == nil && len(content) > 0 {
			prompt = string(content)
		}
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = "Kamu adalah asisten AI WhatsApp bergaya Gen Z Indonesia yang kasual, santai, dan ringkas."
	}
	return &Manager{systemPrompt: prompt}, nil
}

func (m *Manager) GetSystemPrompt() string {
	return m.systemPrompt
}

// BuildConversationContext formats conversation history and user memories into context for Gemini
func (m *Manager) BuildConversationContext(
	history []*storage.Message,
	summary string,
	userMemories []*storage.UserMemory,
	quotedText string,
	trendContext string,
) string {
	var sb strings.Builder

	if summary != "" {
		sb.WriteString("RINGKASAN PERCAKAPAN SEBELUMNYA:\n")
		sb.WriteString(summary)
		sb.WriteString("\n\n")
	}

	if len(userMemories) > 0 {
		sb.WriteString("FAKTA PENGGUNA YANG DIINGAT:\n")
		for _, mem := range userMemories {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", mem.MemoryKey, mem.MemoryValue))
		}
		sb.WriteString("\n")
	}

	if trendContext != "" {
		sb.WriteString("INFORMASI TREN TERVERIFIKASI:\n")
		sb.WriteString(trendContext)
		sb.WriteString("\n\n")
	}

	if quotedText != "" {
		sb.WriteString("PESAN YANG DIBALAS (QUOTED MESSAGE):\n\"")
		sb.WriteString(quotedText)
		sb.WriteString("\"\n\n")
	}

	if len(history) > 0 {
		sb.WriteString("RIWAYAT PERCAKAPAN TERBARU:\n")
		for _, msg := range history {
			role := "User"
			if msg.Direction == "outbound" {
				role = "Kamu (TemanWA)"
			}
			text := msg.TextContent
			if text == "" && msg.MessageType != "text" {
				text = fmt.Sprintf("[%s]", msg.MessageType)
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n", role, text))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
