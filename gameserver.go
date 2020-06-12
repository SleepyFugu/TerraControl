package main

import "regexp"

var gameServers = make([]GameServer, 0)
var gameEvents = make([]*regexp.Regexp, 0)
var illegalNamesRe = make([]*regexp.Regexp, 0)

// GameServer -
type GameServer interface {
	IsUp() bool
	Stop() error
	Start() error
	Restart() error
}

// Commandable -
type Commandable interface {
	EnqueueCommand(string)
}

func superviseCommandQueue(c Commandable) {
	for {
	}
}

// SendCommand -
func SendCommand(s string, cs Commandable) {
	cs.EnqueueCommand(s)
}

// EventType -
func EventType(s string, gs GameServer) int {
	for i, re := range gameEvents {
		if re.MatchString(s) {
			return i
		}
	}
	return -1
}

func init() {
	gameEvents = append(gameEvents, regexp.MustCompile("^.{1,20} has joined.$"))
	illegalNamesRe = append(illegalNamesRe, regexp.MustCompile(
		"^(\\s$|^[<>\\[\\]\\(\\)\\|\\]|[<>\\[\\]\\(\\)\\|\\]$|[aA]dmin|[sS]ystem|[sS]erver|[sS]uper[aA]dmin)"))
}
