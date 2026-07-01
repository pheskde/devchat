package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/pheskde/devchat/internal/store"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const sidebarWidth = 28

const sessionRefreshInterval = 5 * time.Second

var (
	sidebarStyle = lipgloss.NewStyle().
			Width(sidebarWidth).
			Padding(0, 1).
			BorderStyle(lipgloss.NormalBorder()).
			BorderRight(true).
			BorderForeground(lipgloss.Color("240"))

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("212")).
				Bold(true)

	itemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	roleColors = map[string]lipgloss.Color{
		"system":    lipgloss.Color("214"),
		"user":      lipgloss.Color("39"),
		"assistant": lipgloss.Color("112"),
	}
	defaultRoleColor = lipgloss.Color("245")
)

type sessionsLoadedMsg struct {
	sessions []store.Session
	err      error
}

type wsEventMsg struct {
	watchID int
	msg     store.Message
	ok      bool
}

type refreshTickMsg struct{}

type model struct {
	cl      *client
	width   int
	height  int
	ready   bool

	sessions    []store.Session
	selectedIdx int

	messages []store.Message
	vp       viewport.Model

	watchID   int
	watchStop chan struct{}
	watchCh   <-chan store.Message

	statusMsg string
	err       error
}

func newModel(cl *client) model {
	return model{cl: cl, vp: viewport.New(0, 0)}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchSessionsCmd(m.cl), tickRefresh())
}

func tickRefresh() tea.Cmd {
	return tea.Tick(sessionRefreshInterval, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

func fetchSessionsCmd(cl *client) tea.Cmd {
	return func() tea.Msg {
		sessions, err := cl.listSessions()
		return sessionsLoadedMsg{sessions: sessions, err: err}
	}
}

// waitForWS returns a Cmd that blocks for the next message on ch and wraps
// it tagged with watchID, so stale events from a since-replaced watcher can
// be told apart from the current one.
func waitForWS(watchID int, ch <-chan store.Message) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		return wsEventMsg{watchID: watchID, msg: msg, ok: ok}
	}
}

func (m *model) switchSession(id string) tea.Cmd {
	if m.watchStop != nil {
		close(m.watchStop)
	}
	m.watchID++
	m.watchStop = make(chan struct{})
	m.messages = nil
	m.vp.SetContent("")
	m.watchCh = m.cl.watchSession(id, 0, m.watchStop)
	return waitForWS(m.watchID, m.watchCh)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		vpWidth := m.width - sidebarWidth - 4
		if vpWidth < 10 {
			vpWidth = 10
		}
		vpHeight := m.height - 4
		if vpHeight < 3 {
			vpHeight = 3
		}
		m.vp.Width, m.vp.Height = vpWidth, vpHeight
		m.ready = true
		m.vp.SetContent(m.renderMessages())
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.watchStop != nil {
				close(m.watchStop)
			}
			return m, tea.Quit
		case "up", "k":
			if m.selectedIdx > 0 {
				m.selectedIdx--
				return m, m.switchSession(m.sessions[m.selectedIdx].ID)
			}
			return m, nil
		case "down", "j":
			if m.selectedIdx < len(m.sessions)-1 {
				m.selectedIdx++
				return m, m.switchSession(m.sessions[m.selectedIdx].ID)
			}
			return m, nil
		}

	case sessionsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tickRefresh()
		}
		m.err = nil
		prevID := ""
		if m.selectedIdx < len(m.sessions) {
			prevID = m.sessions[m.selectedIdx].ID
		}
		m.sessions = msg.sessions

		if len(m.sessions) == 0 {
			return m, tickRefresh()
		}

		newIdx := 0
		for i, s := range m.sessions {
			if s.ID == prevID {
				newIdx = i
				break
			}
		}
		firstLoad := m.watchStop == nil
		m.selectedIdx = newIdx
		if firstLoad {
			cmd := m.switchSession(m.sessions[m.selectedIdx].ID)
			return m, tea.Batch(cmd, tickRefresh())
		}
		return m, tickRefresh()

	case refreshTickMsg:
		return m, fetchSessionsCmd(m.cl)

	case wsEventMsg:
		if msg.watchID != m.watchID {
			// Stale watcher from a session we've since switched away from.
			return m, nil
		}
		if !msg.ok {
			m.statusMsg = "connection closed"
			return m, nil
		}
		m.messages = append(m.messages, msg.msg)
		m.vp.SetContent(m.renderMessages())
		m.vp.GotoBottom()
		return m, waitForWS(m.watchID, m.watchCh)
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m model) renderMessages() string {
	var b strings.Builder
	for _, msg := range m.messages {
		color, ok := roleColors[msg.Role]
		if !ok {
			color = defaultRoleColor
		}
		roleTag := lipgloss.NewStyle().Foreground(color).Bold(true).Render(fmt.Sprintf("[%s]", msg.Author))
		ts := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(msg.CreatedAt.Local().Format("15:04:05"))
		b.WriteString(fmt.Sprintf("%s %s\n%s\n\n", ts, roleTag, msg.Content))
	}
	return b.String()
}

func (m model) renderSidebar() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Sessions"))
	b.WriteString("\n\n")
	for i, s := range m.sessions {
		line := s.Name
		if line == "" {
			line = s.ID
		}
		if i == m.selectedIdx {
			b.WriteString(selectedItemStyle.Render("> " + line))
		} else {
			b.WriteString(itemStyle.Render("  " + line))
		}
		b.WriteString("\n")
	}
	return sidebarStyle.Height(m.vp.Height).Render(b.String())
}

func (m model) View() string {
	if !m.ready {
		return "loading devchat...\n"
	}

	header := headerStyle.Width(m.width).Render(fmt.Sprintf("devchat viewer  -  %s", m.cl.apiBase))

	status := m.statusMsg
	if m.err != nil {
		status = "error: " + m.err.Error()
	}
	footer := footerStyle.Render(fmt.Sprintf("up/down: switch session   q: quit   %s", status))

	body := lipgloss.JoinHorizontal(lipgloss.Top, m.renderSidebar(), m.vp.View())

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}
