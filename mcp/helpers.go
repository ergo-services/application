package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"ergo.services/ergo/gen"
)

// parsePID parses a PID string like "<ABCDEF12.0.1003>"
func parsePID(node gen.Atom, creation int64, s string) (gen.PID, error) {
	pid := gen.PID{
		Node:     node,
		Creation: creation,
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return pid, errors.New("incorrect PID format, expected <crc.high.low>")
	}

	id1, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return pid, err
	}
	id2, err := strconv.ParseInt(strings.TrimSuffix(parts[2], ">"), 10, 64)
	if err != nil {
		return pid, err
	}
	pid.ID = (uint64(id1) << 32) | (uint64(id2))
	return pid, nil
}

// parseAlias parses an Alias string like "Alias#<ABCDEF12.0.1.2>"
func parseAlias(node gen.Atom, creation int64, s string) (gen.Alias, error) {
	alias := gen.Alias{
		Node:     node,
		Creation: creation,
	}

	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return alias, errors.New("incorrect Alias format, expected <crc.id0.id1.id2>")
	}

	id1, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return alias, err
	}
	id2, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return alias, err
	}
	id3, err := strconv.ParseInt(strings.TrimSuffix(parts[3], ">"), 10, 64)
	if err != nil {
		return alias, err
	}
	alias.ID[0] = uint64(id1)
	alias.ID[1] = uint64(id2)
	alias.ID[2] = uint64(id3)
	return alias, nil
}

func generateSamplerID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
