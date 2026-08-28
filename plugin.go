package main

import "github.com/rulego/rulego/api/types"

// Plugins is the symbol loaded by RuleGo's Go plugin registry.
var Plugins pluginRegistry

type pluginRegistry struct{}

func (*pluginRegistry) Init() error { return nil }

func (*pluginRegistry) Components() []types.Node {
	return []types.Node{&indexedVodNode{}}
}
