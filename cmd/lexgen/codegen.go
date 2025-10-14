package main

import (
	"io"
)

type GenConfig struct {
}

type Generator struct {
	Config *GenConfig
	Lex    *FlatLexicon
	Out    io.Writer
}
