package node

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const usersMarker = "# NAIVE_USERS"

func renderTemplate(template string, users []User) ([]byte, error) {
	if len(template) > 4<<20 || strings.Count(template, usersMarker) != 1 {
		return nil, errors.New("invalid_template")
	}
	lines := strings.Split(template, "\n")
	marker := -1
	// The marker must be on its own line directly in a forward_proxy block.
	// This small lexer tracks braces while ignoring quoted values and comments;
	// Caddy remains authoritative via validate on the full rendered config.
	var stack []string
	var blockIDs []int
	blockID := 0
	markerBlock := 0
	authBlocks := make(map[int]bool)
	var pending []string
	for i, line := range lines {
		if strings.TrimSpace(line) == usersMarker {
			if len(stack) == 0 || stack[len(stack)-1] != "forward_proxy" {
				return nil, errors.New("invalid_template")
			}
			marker = i
			markerBlock = blockIDs[len(blockIDs)-1]
			continue
		}
		tokens, err := templateTokens(line)
		if err != nil {
			return nil, err
		}
		for _, token := range tokens {
			switch token {
			case "{":
				name := ""
				if len(pending) > 0 {
					name = pending[0]
				}
				stack = append(stack, name)
				blockID++
				blockIDs = append(blockIDs, blockID)
				pending = nil
			case "}":
				if len(stack) == 0 {
					return nil, errors.New("invalid_template")
				}
				stack = stack[:len(stack)-1]
				blockIDs = blockIDs[:len(blockIDs)-1]
				pending = nil
			default:
				// All proxy authentication belongs to the managed user set.
				if len(stack) > 0 && stack[len(stack)-1] == "forward_proxy" && token == "basic_auth" && len(pending) == 0 {
					authBlocks[blockIDs[len(blockIDs)-1]] = true
				}
				pending = append(pending, token)
			}
		}
		pending = nil
	}
	if marker < 0 || len(stack) != 0 || authBlocks[markerBlock] {
		return nil, errors.New("invalid_template")
	}
	indent := lines[marker][:len(lines[marker])-len(strings.TrimLeft(lines[marker], " \t"))]
	if len(users) == 0 {
		var entropy [48]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return nil, err
		}
		// An empty auth list disables forwardproxy authentication. Keep a random
		// inaccessible credential so revoking the last user never opens a proxy.
		users = []User{{Username: "deny_" + hex.EncodeToString(entropy[:16]), Password: hex.EncodeToString(entropy[16:])}}
	}
	var auth strings.Builder
	for i, user := range users {
		if i > 0 {
			auth.WriteByte('\n')
		}
		fmt.Fprintf(&auth, "%sbasic_auth %s %s", indent, user.Username, user.Password)
	}
	lines[marker] = auth.String()
	return []byte(strings.Join(lines, "\n")), nil
}

func templateTokens(line string) ([]string, error) {
	var tokens []string
	var token strings.Builder
	var quote rune
	escape := false
	flush := func() {
		if token.Len() > 0 {
			tokens = append(tokens, token.String())
			token.Reset()
		}
	}
	for _, r := range line {
		if quote != 0 {
			token.WriteRune(r)
			if escape {
				escape = false
			} else if r == '\\' && quote == '"' {
				escape = true
			} else if r == quote {
				quote = 0
			}
			continue
		}
		if r == '#' {
			break
		}
		switch r {
		case '"', '`':
			quote = r
			token.WriteRune(r)
		case ' ', '\t', '\r':
			flush()
		default:
			token.WriteRune(r)
		}
	}
	flush()
	if quote != 0 {
		return nil, errors.New("invalid_template")
	}
	return tokens, nil
}
