package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

func TestPluginContract(t *testing.T) {
	components := Plugins.Components()
	if len(components) != 1 || components[0].Type() != "indexedVod" {
		t.Fatalf("components = %#v", components)
	}
	node, ok := components[0].(*indexedVodNode)
	if !ok {
		t.Fatalf("component type = %T", components[0])
	}
	want := []string{types.Success, types.Failure}
	if got := *node.Def().RelationTypes; !reflect.DeepEqual(got, want) {
		t.Fatalf("relations = %#v, want %#v", got, want)
	}
}

func TestExampleScriptsCompile(t *testing.T) {
	raw, err := os.ReadFile("examples/youtube-hls/chain.json")
	if err != nil {
		t.Fatal(err)
	}
	var example struct {
		Metadata struct {
			Nodes     []json.RawMessage `json:"nodes"`
			Endpoints []struct {
				Routers []struct {
					ID string `json:"id"`
					To struct {
						Processors []string `json:"processors"`
					} `json:"to"`
				} `json:"routers"`
			} `json:"endpoints"`
			Connections []struct {
				From string `json:"fromId"`
				To   string `json:"toId"`
			} `json:"connections"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &example); err != nil {
		t.Fatal(err)
	}
	var normalize, parentAcquireRequest, segmentSourceSwitch, cachedProduceRequest, parentReadySwitch, initialResolveRequest json.RawMessage
	ids := make(map[string]bool)
	for _, node := range example.Metadata.Nodes {
		var identity struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(node, &identity); err != nil {
			t.Fatal(err)
		}
		if identity.ID == "" || ids[identity.ID] {
			t.Fatalf("invalid or duplicate example node ID %q", identity.ID)
		}
		ids[identity.ID] = true
		if identity.Type != "jsTransform" && identity.Type != "jsSwitch" {
			continue
		}
		if identity.ID == "normalize-source" {
			normalize = node
		}
		if identity.ID == "parent-acquire-request" {
			parentAcquireRequest = node
		}
		if identity.ID == "segment-source-switch" {
			segmentSourceSwitch = node
		}
		if identity.ID == "cached-produce-request" {
			cachedProduceRequest = node
		}
		if identity.ID == "parent-ready-switch" {
			parentReadySwitch = node
		}
		if identity.ID == "initial-resolve-request" {
			initialResolveRequest = node
		}
		dsl, err := json.Marshal(map[string]any{
			"ruleChain": map[string]any{"id": "script-" + identity.ID, "root": true},
			"metadata":  map[string]any{"firstNodeIndex": 0, "nodes": []json.RawMessage{node}},
		})
		if err != nil {
			t.Fatal(err)
		}
		pool := rulego.NewRuleGo()
		engine, err := pool.New("script-"+identity.ID, dsl, rulego.WithConfig(rulego.NewConfig()))
		if err != nil {
			t.Fatalf("%s: %v", identity.ID, err)
		}
		engine.Stop(nil)
	}
	for _, connection := range example.Metadata.Connections {
		if !ids[connection.From] || !ids[connection.To] {
			t.Fatalf("dangling example connection %q -> %q", connection.From, connection.To)
		}
	}
	indegree := make(map[string]int, len(ids))
	edges := make(map[string][]string, len(ids))
	for id := range ids {
		indegree[id] = 0
	}
	for _, connection := range example.Metadata.Connections {
		edges[connection.From] = append(edges[connection.From], connection.To)
		indegree[connection.To]++
	}
	queue := make([]string, 0, len(ids))
	for id, count := range indegree {
		if count == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range edges[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(ids) {
		t.Fatal("example rule chain contains a cycle")
	}
	if got := example.Metadata.Endpoints[0].Routers[0].To.Processors; !reflect.DeepEqual(got, []string{"metadataToHeaders", "responseToBody"}) {
		t.Fatalf("manifest response processors = %#v", got)
	}
	if normalize == nil {
		t.Fatal("normalize-source node is missing")
	}
	dsl, err := json.Marshal(map[string]any{
		"ruleChain": map[string]any{"id": "normalize-source-test", "root": true},
		"metadata": map[string]any{
			"firstNodeIndex": 0,
			"nodes": []json.RawMessage{
				normalize,
				json.RawMessage(`{"id":"end","type":"end","configuration":{}}`),
			},
			"connections": []map[string]string{{"fromId": "normalize-source", "toId": "end", "type": types.Success}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pool := rulego.NewRuleGo()
	engine, err := pool.New("normalize-source-test", dsl, rulego.WithConfig(rulego.NewConfig()))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(nil)
	stdout := `{"id":"Z4tHPyZBC8g","requested_formats":[{"url":"https://media.invalid/v","ext":"mp4","vcodec":"avc1.64002a","acodec":"none","http_headers":{"X-Test":"video"}},{"url":"https://media.invalid/a","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","http_headers":{"X-Test":"audio"}}]}`
	body, _ := json.Marshal(map[string]any{"exit_code": 0, "stdout": stdout})
	input := types.NewMsgWithJsonData(string(body))
	input.Metadata.PutValue("videoId", "Z4tHPyZBC8g")
	var output types.RuleMsg
	var callbackErr error
	engine.OnMsgAndWait(input, types.WithOnEnd(func(_ types.RuleContext, msg types.RuleMsg, err error, _ string) {
		output, callbackErr = msg, err
	}))
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	var request inspectRequest
	if err := json.Unmarshal(output.GetBytes(), &request); err != nil {
		t.Fatal(err)
	}
	if request.Operation != "inspect" || request.Source.SourceKey != "youtube:Z4tHPyZBC8g" || request.Source.Video.URL != "https://media.invalid/v" || request.Source.Audio.URL != "https://media.invalid/a" {
		t.Fatalf("normalized request = %#v", request)
	}
	if parentAcquireRequest == nil || segmentSourceSwitch == nil || cachedProduceRequest == nil {
		t.Fatal("source lease cache nodes are missing")
	}
	revision := strings.Repeat("a", 64)
	dsl, err = json.Marshal(map[string]any{
		"ruleChain": map[string]any{"id": "source-lease-cache-test", "root": true},
		"metadata": map[string]any{
			"firstNodeIndex": 0,
			"nodes": []json.RawMessage{
				parentAcquireRequest,
				segmentSourceSwitch,
				cachedProduceRequest,
				json.RawMessage(`{"id":"end","type":"end","configuration":{}}`),
			},
			"connections": []map[string]string{
				{"fromId": "parent-acquire-request", "toId": "segment-source-switch", "type": types.Success},
				{"fromId": "segment-source-switch", "toId": "cached-produce-request", "type": "Cached"},
				{"fromId": "cached-produce-request", "toId": "end", "type": types.Success},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine, err = pool.New("source-lease-cache-test", dsl, rulego.WithConfig(rulego.NewConfig()))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(nil)
	sourceJSON, _ := json.Marshal(request.Source)
	input = types.NewMsgWithJsonData(`{"revision":"` + revision + `","segments":[{"duration":2}]}`)
	input.Metadata.PutValue("videoId", "Z4tHPyZBC8g")
	input.Metadata.PutValue("source", string(sourceJSON))
	var cachedValue any
	var cacheErr error
	engine.OnMsgAndWait(input, types.WithStartNode("parent-acquire-request"), types.WithSkipTellNext(), types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, _ string) {
		output, callbackErr = msg, err
		cachedValue, cacheErr = ctx.ChainCache().Get("indexed-vod:lease:Z4tHPyZBC8g:" + revision)
	}))
	if callbackErr != nil || cacheErr != nil || cachedValue != string(sourceJSON) {
		t.Fatalf("cached source=%#v callbackErr=%v cacheErr=%v", cachedValue, callbackErr, cacheErr)
	}

	input = types.NewMsgWithJsonData(`{}`)
	input.Metadata.PutValue("videoId", "Z4tHPyZBC8g")
	input.Metadata.PutValue("revision", revision)
	input.Metadata.PutValue("segment", "1")
	input.Metadata.PutValue("stagingDir", "/tmp/generation")
	input.Metadata.PutValue("maxBytes", "1024")
	input.Metadata.PutValue("publishBy", "2026-08-28T18:00:00Z")
	callbackErr = nil
	engine.OnMsgAndWait(input, types.WithStartNode("segment-source-switch"), types.WithOnEnd(func(_ types.RuleContext, msg types.RuleMsg, err error, _ string) {
		output, callbackErr = msg, err
	}))
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	var cached produceRequest
	if err := json.Unmarshal(output.GetBytes(), &cached); err != nil {
		t.Fatal(err)
	}
	if cached.Operation != "produce" || cached.ExpectedRevision != revision || cached.Segment != 1 || cached.Source.Video.URL != request.Source.Video.URL {
		t.Fatalf("cached production request = %#v", cached)
	}

	if parentReadySwitch == nil || initialResolveRequest == nil {
		t.Fatal("initial member routing nodes are missing")
	}
	dsl, err = json.Marshal(map[string]any{
		"ruleChain": map[string]any{"id": "initial-resolve-binding-test", "root": true},
		"metadata": map[string]any{
			"firstNodeIndex": 0,
			"nodes": []json.RawMessage{
				parentReadySwitch,
				initialResolveRequest,
				json.RawMessage(`{"id":"end","type":"end","configuration":{}}`),
			},
			"connections": []map[string]string{
				{"fromId": "parent-ready-switch", "toId": "initial-resolve-request", "type": "Initial"},
				{"fromId": "initial-resolve-request", "toId": "end", "type": types.Success},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine, err = pool.New("initial-resolve-binding-test", dsl, rulego.WithConfig(rulego.NewConfig()))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(nil)
	input = types.NewMsgWithJsonData(`{"resourceId":"parent-resource-id"}`)
	input.Metadata.PutValue("segment", "0")
	engine.OnMsgAndWait(input, types.WithOnEnd(func(_ types.RuleContext, msg types.RuleMsg, err error, _ string) {
		output, callbackErr = msg, err
	}))
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	var resolve struct {
		Operation  string `json:"operation"`
		ResourceID string `json:"resourceId"`
		Member     string `json:"member"`
	}
	if err := json.Unmarshal(output.GetBytes(), &resolve); err != nil {
		t.Fatal(err)
	}
	if resolve.Operation != "resolve" || resolve.ResourceID != "parent-resource-id" || resolve.Member != "0.ts" {
		t.Fatalf("initial resolve request = %#v", resolve)
	}
}

func TestRuleGoSharedOwnerAndBorrower(t *testing.T) {
	if err := rulego.Registry.Register(&indexedVodNode{}); err != nil {
		t.Fatal(err)
	}
	defer rulego.Registry.Unregister(componentType)
	dsl := fmt.Sprintf(`{
  "ruleChain":{"id":"indexed-vod-test","root":true},
  "metadata":{
    "firstNodeIndex":1,
    "nodes":[
      {"id":"owner","type":"indexedVod","configuration":{
        "root":%q,"ffmpegAddress":"127.0.0.1:1","ffmpegSecret":"test"}},
      {"id":"borrower","type":"indexedVod","configuration":{"root":"ref://owner"}},
      {"id":"end","type":"end","configuration":{}}
    ],
    "connections":[
      {"fromId":"borrower","toId":"end","type":"Success"},
      {"fromId":"borrower","toId":"end","type":"Failure"}
    ]
  }
}`, t.TempDir())
	pool := rulego.NewRuleGo()
	engine, err := pool.New("indexed-vod-test", []byte(dsl), rulego.WithConfig(rulego.NewConfig()))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(nil)

	var output types.RuleMsg
	var relation string
	engine.OnMsgAndWait(types.NewMsgWithJsonData(`{}`), types.WithOnEnd(func(_ types.RuleContext, msg types.RuleMsg, _ error, gotRelation string) {
		output, relation = msg, gotRelation
	}))
	var failure nodeError
	if err := json.Unmarshal(output.GetBytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if relation != types.Failure || failure.Kind != "invalid_input" {
		t.Fatalf("relation=%q failure=%#v", relation, failure)
	}
}
