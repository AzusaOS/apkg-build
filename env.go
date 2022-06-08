package main

import (
	"strings"

	"mvdan.cc/sh/v3/shell"
)

func (e *buildEnv) applyEnv() error {
	for _, setv := range e.i.Env {
		p := strings.IndexByte(setv, '=')
		if p == -1 {
			// wtf?
			continue
		}

		k := setv[:p]
		v := setv[p+1:]

		v, err := shell.Expand(v, e.getVar)
		if err != nil {
			return err
		}

		e.vars[k] = v
		switch k {
		case "S":
			e.src = v
		}
	}
	return nil
}
