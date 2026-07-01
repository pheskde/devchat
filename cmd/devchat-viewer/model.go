package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/pheskde/devchat/internal/store"

	"github.com/atotto/clipboard"
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

	presenceStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("112"))
	presenceDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

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

type inviteLoadedMsg struct {
	text string
	err  error
}

type presenceLoadedMsg struct {
	sessionID string
	presence  []store.Presence
	err       error
}

type model struct {
	cl         *client
	viewerName string
	width      int
	height     int
	ready      bool

	sessions    []store.Session
	selectedIdx int

	messages []store.Message
	vp       viewport.Model

	watchID   int
	watchStop chan struct{}
	watchCh   <-chan store.Message

	presence []store.Presence

	statusMsg string
	err       error

	showInvite bool
	inviteText string
}

func newModel(cl *client, viewerName string) model {
	return model{cl: cl, viewerName: viewerName, vp: viewport.New(0, 0)}
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

func fetchInviteCmd(cl *client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		text, err := cl.getInvite(sessionID)
		return inviteLoadedMsg{text: text, err: err}
	}
}

func fetchPresenceCmd(cl *client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		presence, err := cl.listPresence(sessionID)
		return presenceLoadedMsg{sessionID: sessionID, presence: presence, err: err}
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
	m.presence = nil
	m.watchCh = m.cl.watchSession(id, 0, m.viewerName, m.watchStop)
	return tea.Batch(waitForWS(m.watchID, m.watchCh), fetchPresenceCmd(m.cl, id))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		vpWidth := max(m.width-sidebarWidth-4, 10)
		vpHeight := max(m.height-5, 3)
		m.vp.Width, m.vp.Height = vpWidth, vpHeight
		m.ready = true
		m.vp.SetContent(m.renderMessages())
		return m, nil

	case tea.KeyMsg:
		if m.showInvite {
			switch msg.String() {
			case "ctrl+c", "q":
				if m.watchStop != nil {
					close(m.watchStop)
				}
				return m, tea.Quit
			case "esc", "i":
				m.showInvite = false
				return m, nil
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "q":
			if m.watchStop != nil {
				close(m.watchStop)
			}
			return m, tea.Quit
		case "i":
			if len(m.sessions) == 0 {
				return m, nil
			}
			return m, fetchInviteCmd(m.cl, m.sessions[m.selectedIdx].ID)
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
		// The next tick is scheduled once fetchSessionsCmd's result comes back
		// (see sessionsLoadedMsg below), so the loop keeps going without
		// scheduling duplicate ticks here.
		cmds := []tea.Cmd{fetchSessionsCmd(m.cl)}
		if m.selectedIdx < len(m.sessions) {
			cmds = append(cmds, fetchPresenceCmd(m.cl, m.sessions[m.selectedIdx].ID))
		}
		return m, tea.Batch(cmds...)

	case presenceLoadedMsg:
		if msg.err != nil || m.selectedIdx >= len(m.sessions) || msg.sessionID != m.sessions[m.selectedIdx].ID {
			// Stale response for a session we've since switched away from.
			return m, nil
		}
		m.presence = msg.presence
		return m, nil

	case inviteLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "invite error: " + msg.err.Error()
			return m, nil
		}
		m.inviteText = msg.text
		m.showInvite = true
		if err := clipboard.WriteAll(msg.text); err != nil {
			m.statusMsg = "invite shown (clipboard copy failed: " + err.Error() + ")"
		} else {
			m.statusMsg = "invite copied to clipboard"
		}
		return m, nil

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
		fmt.Fprintf(&b, "%s %s\n%s\n\n", ts, roleTag, msg.Content)
	}
	return b.String()
}

func (m model) renderPresence() string {
	var active []string
	for _, p := range m.presence {
		if p.Active {
			active = append(active, p.Author)
		}
	}
	if len(active) == 0 {
		return presenceDim.Render("active now: (none)")
	}
	return presenceStyle.Render("active now: "+strings.Join(active, ", ")) +
		presenceDim.Render(fmt.Sprintf("  (%d)", len(active)))
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
	presenceBar := lipgloss.NewStyle().Padding(0, 1).Render(m.renderPresence())

	if m.showInvite {
		box := lipgloss.NewStyle().
			Width(m.width - 2).
			Height(m.vp.Height).
			Padding(1, 2).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("212")).
			Render("Einladungstext zum Einfuegen in ein LLM/einen Agenten:\n\n" + m.inviteText)
		footer := footerStyle.Render("esc/i: zurueck   q: quit")
		return lipgloss.JoinVertical(lipgloss.Left, header, box, footer)
	}

	status := m.statusMsg
	if m.err != nil {
		status = "error: " + m.err.Error()
	}
	footer := footerStyle.Render(fmt.Sprintf("up/down: switch session   i: copy invite   q: quit   %s", status))

	body := lipgloss.JoinHorizontal(lipgloss.Top, m.renderSidebar(), m.vp.View())

	return lipgloss.JoinVertical(lipgloss.Left, header, presenceBar, body, footer)
}
