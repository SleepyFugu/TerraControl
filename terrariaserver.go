package main

import (
	"bufio"
	"errors"
	"io"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// TerrariaPlayer - Defines a player that has connected to the server at some point
type TerrariaPlayer struct {
	// Make sure we implement Player
	Player

	ip     net.IP
	name   string
	server *TerrariaServer
}

// SetIP - Sets or updates a players IP address
func (p TerrariaPlayer) SetIP(ips string) {
	p.ip = net.ParseIP(ips)
}

// IP - Returns the IP address that the player used to connect this session
func (p TerrariaPlayer) IP() net.IP {
	return p.ip
}

// Name - Return the name of the player object
func (p TerrariaPlayer) Name() string {
	return p.name
}

// Kick - Kick a player
func (p TerrariaPlayer) Kick(r string) {
	SendCommand(sprintf("say Kicking player: \"%s\". %s.", p.Name(), r), p.server)
	SendCommand("kick "+p.Name(), p.server)
}

// Ban - Ban a player
func (p TerrariaPlayer) Ban(r string) {
	SendCommand(sprintf("say Banning player: \"%s\". %s.", p.Name(), r), p.server)
	SendCommand("ban "+p.Name(), p.server)
}

// TerrariaServer - Terraria server definition
type TerrariaServer struct {
	Cmd    *exec.Cmd
	Stdin  io.Writer
	Stdout io.Reader

	// Loggable
	loglevel int
	uuid     string

	// Commandable
	commandqueue    chan string
	commandcount    int
	commandqueuemax int

	// PlayerInfo
	players  []*TerrariaPlayer
	messages [][2]string

	// Config
	worldfile  string
	configfile string

	// Game State
	password string
	version  string
	seed     string
	motd     string
	time     string
}

// Start -
func (s *TerrariaServer) Start() error {
	var err error

	if s.Stdin, err = s.Cmd.StdinPipe(); err != nil {
		return err
	}

	if s.Stdout, err = s.Cmd.StdoutPipe(); err != nil {
		return err
	}

	s.commandqueue = make(chan string, 500)
	s.commandcount = 0
	s.commandqueuemax = 500
	s.motd = "<default>"

	ready := make(chan struct{})

	// Refactor these two goroutines to exit gracefully when the
	// server is stopped to avoid stale goroutines
	go superviseTerrariaOut(s, ready)
	go func() {
		for {
			select {
			case cmd := <-s.commandqueue:
				time.Sleep(time.Second / 2)
				b := convertString(cmd)
				b.WriteTo(s.Stdin)
				LogDebug(s, "Ran: "+cmd)
				s.commandcount = s.commandcount - 1
			}
		}
	}()

	if err = s.Cmd.Start(); err != nil {
		return err
	}

	<-ready

	// Output commands that we'll use to populate the objects DB
	SendCommand("seed", s)
	SendCommand("version", s)
	SendCommand("password", s)

	return nil
}

// Stop -
func (s *TerrariaServer) Stop() error {
	LogOutput(s, "Stopping Terraria server")
	done := make(chan error)

	SendCommand("exit", s)
	go func() { done <- s.Cmd.Wait() }()

	LogDebug(s, "Waiting for Terraria to exit")
	select {
	case <-time.After(30 * time.Second):
		s.Cmd.Process.Kill()
		return errors.New("terraria took too long to exit, killed")
	case err := <-done:
		LogInfo(s, "Terraria server has been stopped")
		if err != nil {
			return err
		}
		return nil
	}
}

// Restart -
func (s *TerrariaServer) Restart() error {
	if err := s.Stop(); err != nil {
		return err
	}

	if err := s.Start(); err != nil {
		return err
	}

	return nil
}

// Status -
func (s *TerrariaServer) Status() (int, error) {
	if s.Cmd.ProcessState != nil {
		return 1, nil
	}

	if s.Cmd.Process != nil {
		return 0, nil
	}

	return 2, errors.New("Process entered an unknown state")
}

// IsUp -
func (s *TerrariaServer) IsUp() bool {
	if s.Cmd.ProcessState != nil {
		return false
	}

	if s.Cmd.Process != nil {
		return true
	}

	return false
}

/**********/
/* Loggable */
/**********/

// UUID -
func (s *TerrariaServer) UUID() string {
	return s.uuid
}

// Loglevel -
func (s *TerrariaServer) Loglevel() int {
	return s.loglevel
}

// SetLoglevel -
func (s *TerrariaServer) SetLoglevel(l int) {
	s.loglevel = l
}

/***************/
/* Commandable */
/***************/

// EnqueueCommand -
func (s *TerrariaServer) EnqueueCommand(c string) {
	if s.commandcount < s.commandqueuemax-1 {
		s.commandqueue <- c + "\n"
		s.commandcount = s.commandcount + 1
	} else {
		LogWarning(s, "Attempted to run more than the maximum amount of commands!")
	}
}

/*************/
/* Versioned */
/*************/

// SetVersion - Sets the current version of the Terraria server
func (s *TerrariaServer) SetVersion(v string) {
	s.version = v
}

// Version - Return the version of the Terraria server
func (s *TerrariaServer) Version() string {
	return s.version
}

/********/
/* Main */
/********/

// Player - Return a player object that matches the string given
func (s *TerrariaServer) Player(n string) Player {
	for _, p := range s.players {
		if p.Name() == n {
			return p
		}
	}

	return nil
}

// Players - Returns the players that are currently in-game
func (s *TerrariaServer) Players() []Player {
	v := make([]Player, 0)
	for _, t := range s.players {
		v = append(v, *t)
	}
	return v
}

// NewPlayer - Add a player to the list of players if it isn't already present
func (s *TerrariaServer) NewPlayer(n, ips string) Player {
	if p := s.Player(n); p != nil {
		p.SetIP(ips)
		return p
	}

	plr := &TerrariaPlayer{name: n, server: s}
	s.players = append(s.players, plr)

	plr.ip = net.ParseIP(ips)
	LogInfo(s, "New player logged: "+plr.Name())
	return plr
}

// RemovePlayer - Removes a player from the list of players
func (s *TerrariaServer) RemovePlayer(n string) bool {
	for i, p := range s.players {
		if p.Name() == n {
			LogInfo(s, "Removing "+p.Name())
			s.players = append(s.players[:i], s.players[i+1:]...)
			return true
		}
	}
	return false
}

// ChatMessages - Return the total number of message that are logged
func (s *TerrariaServer) ChatMessages() [][2]string {
	return s.messages
}

// NewChatMessage - Return the total number of message that are logged
func (s *TerrariaServer) NewChatMessage(msg, name string) {
	s.messages = append(s.messages, [2]string{name, msg})
}

// NewTerrariaServer -
func NewTerrariaServer(path string, args ...string) *TerrariaServer {
	t := &TerrariaServer{
		uuid: "terraria",
		Cmd: exec.Command(path,
			"-autocreate", "3", "-world", "C:\\Users\\Andrew Wyatt\\Documents\\My Games\\Terraria\\Worlds\\World11.wld", "-secure",
			"-players", "8", "-pass", "123123", "-noupnp")}

	t.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	t.SetLoglevel(3)

	gameServers = append(gameServers, t)
	return t
}

/**************/
/* Goroutines */
/**************/

func superviseTerrariaOut(s *TerrariaServer, ready chan struct{}) {
	LogDebug(s, "Started Terraria supervisor")
	scanner := bufio.NewScanner(s.Stdout)

	cch := make(chan string, 0) // Initial Connection
	pch := make(chan string, 0) // Player Login
	go superviseTerrariaConnects(s, cch, pch)

	for scanner.Scan() {
		// Strip the prefix that terraria outputs on a newline. Terraria sometimes
		// throws extras, so just loop until theyre all gone.
		out := scanner.Text()
		for strings.HasPrefix(out, ":") {
			out = strings.TrimPrefix(out, ":")
			out = strings.TrimSpace(out)
		}

		select {
		// Once we're ready, start processing logs.
		case <-ready:
			e := GetEventFromString(out)
			switch e.name {
			case "EventConnection":
				e.Handler(s, out, cch)
			case "EventPlayerInfo":
				e.Handler(s, out, pch)
			default:
				e.Handler(s, out, nil)
			}

		// Output as INIT until the server is ready
		default:
			switch out {
			case "Server started":
				LogInit(s, "Terraria server INIT completed")
				close(ready) //Close the channel to close this path

			default:
				LogInit(s, out)
			}
		}
	}
}

func superviseTerrariaConnects(s *TerrariaServer, cch chan string, pch chan string) {
	newconnections := make(map[string]time.Time)
	stale := make(map[string]int)
	conRe := gameEventsMap["EventPlayerInfo"]

	for {
		select {
		case <-time.After(5 * time.Second):
			for ip, t := range newconnections {
				now := time.Now()
				if now.Sub(t) > 30*time.Second {
					LogWarning(s, "Stale connection found for IP: "+ip)
					delete(newconnections, ip)
					if num, ok := stale[ip]; ok {
						stale[ip] = num + 1
					} else {
						stale[ip] = 1
					}
				}
			}

			for ip, cnt := range stale {
				if cnt > 25 {
					LogWarning(s, "Possible DoS taking place!")
					LogWarning(s, sprintf("IP: %s | Stale Connections: %d", ip, cnt))
				}
				delete(stale, ip)
			}

		case c := <-cch:
			LogDebug(s, "Adding channeled connection to list")
			if _, ok := newconnections[c]; ok {
				LogWarning(s, "Extra connection found for IP: "+c)
				if num, ok := stale[c]; ok {
					stale[c] = num + 1
				} else {
					stale[c] = 1
				}

			}
			newconnections[c] = time.Now()

		case plr := <-pch:
			LogDebug(s, "Received player info: "+plr)
			m := conRe.Capture.FindStringSubmatch(plr)
			ip := m[2]
			name := m[1]
			if _, ok := newconnections[ip]; ok {
				delete(newconnections, ip)
				LogDebug(s, sprintf("Removed connection for IP: %s [%s]", ip, name))
			}

			if _, ok := stale[ip]; ok {
				delete(stale, ip)
				LogDebug(s, "Cleared stale connection count for IP: "+ip)
			}
		}
	}
}
