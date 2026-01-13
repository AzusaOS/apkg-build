package main

import (
	"bytes"
	"strings"
)

func shellQuoteEnv(env ...string) string {
	cmd := &bytes.Buffer{}
	cmd.WriteString("env ")
	for _, arg := range env {
		p := strings.IndexByte(arg, '=')
		if p == -1 {
			continue
		}
		cmd.WriteString(arg[:p+1] + shellQuote(arg[p+1:]) + " ")
	}
	return cmd.String()
}

func shellQuoteCmd(args ...string) string {
	cmd := &bytes.Buffer{}
	for _, arg := range args {
		if cmd.Len() > 0 {
			cmd.WriteByte(' ')
		}
		cmd.WriteString(shellQuote(arg))
	}
	return cmd.String()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
