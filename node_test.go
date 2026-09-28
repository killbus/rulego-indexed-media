package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	ffmpegclient "github.com/killbus/rulego-ffmpeg-over-ip/client"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	ruleengine "github.com/rulego/rulego/engine"
)

func TestPluginContract(t *testing.T) {
	components := Plugins.Components()
	if len(components) != 1 || components[0].Type() != "indexedMedia" {
		t.Fatalf("components = %#v", components)
	}
	node, ok := components[0].(*indexedMediaNode)
	if !ok {
		t.Fatalf("component type = %T", components[0])
	}
	if def := node.Def(); def.Type != "indexedMedia" || def.Label != "Indexed Media" {
		t.Fatalf("component definition = %#v", def)
	}
	want := []string{types.Success, types.Failure}
	if got := node.Def().RelationTypes; got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("relations = %#v, want %#v", got, want)
	}
	registry := &ruleengine.RuleComponentRegistry{}
	for _, component := range components {
		if err := registry.Register(component); err != nil {
			t.Fatal(err)
		}
	}
	config := rulego.NewConfig()
	config.ComponentsRegistry = registry
	for _, nodeType := range []string{"indexedMedia", "indexedVod"} {
		t.Run(nodeType, func(t *testing.T) {
			dsl := fmt.Sprintf(`{
  "ruleChain":{"id":"registration-test","root":true},
  "metadata":{"firstNodeIndex":0,"nodes":[
    {"id":"owner","type":%q,"configuration":{
      "root":%q,"ffmpegAddress":"127.0.0.1:1","ffmpegSecret":"test"}}
  ]}
}`, nodeType, t.TempDir())
			pool := rulego.NewRuleGo()
			engine, err := pool.New("registration-test", []byte(dsl), rulego.WithConfig(config))
			if err == nil {
				defer engine.Stop(nil)
			}
			if nodeType == "indexedMedia" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "component not found") || !strings.Contains(err.Error(), nodeType) {
				t.Fatalf("old type load error = %v, want unregistered component", err)
			}
		})
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
	leaseExpiresAt := output.GetMetadata().GetValue("leaseExpiresAtMs")
	leaseTTL, err := time.ParseDuration(output.GetMetadata().GetValue("leaseCacheTtl"))
	if err != nil || leaseTTL < 9*time.Minute || leaseTTL > 10*time.Minute {
		t.Fatalf("fallback lease ttl = %v, err = %v", leaseTTL, err)
	}
	if parentAcquireRequest == nil || segmentSourceSwitch == nil || cachedProduceRequest == nil {
		t.Fatal("source lease cache nodes are missing")
	}
	segmentResolveRequest := exampleScriptNodes(t, "segment-resolve-request")["segment-resolve-request"]
	revision := strings.Repeat("a", 64)
	dsl, err = json.Marshal(map[string]any{
		"ruleChain": map[string]any{"id": "source-lease-cache-test", "root": true},
		"metadata": map[string]any{
			"firstNodeIndex": 0,
			"nodes": []json.RawMessage{
				parentAcquireRequest,
				segmentSourceSwitch,
				cachedProduceRequest,
				segmentResolveRequest,
				json.RawMessage(`{"id":"seed-legacy-lease","type":"jsTransform","configuration":{"jsScript":"$ctx.ChainCache().Set('indexed-vod:lease:'+metadata.videoId+':'+metadata.revision,JSON.stringify(msg),'1h');return {'msg':msg,'metadata':metadata,'msgType':msgType,'dataType':dataType};"}}`),
				json.RawMessage(`{"id":"end","type":"end","configuration":{}}`),
			},
			"connections": []map[string]string{
				{"fromId": "parent-acquire-request", "toId": "segment-source-switch", "type": types.Success},
				{"fromId": "segment-source-switch", "toId": "cached-produce-request", "type": "Cached"},
				{"fromId": "segment-source-switch", "toId": "segment-resolve-request", "type": "Resolve"},
				{"fromId": "segment-resolve-request", "toId": "end", "type": types.Success},
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
	for _, legacy := range []bool{false, true} {
		input = types.NewMsgWithJsonData(string(sourceJSON))
		input.Metadata.PutValue("videoId", "Z4tHPyZBC8g")
		input.Metadata.PutValue("revision", revision)
		if legacy {
			engine.OnMsgAndWait(input, types.WithStartNode("seed-legacy-lease"), types.WithSkipTellNext(), types.WithOnEnd(func(_ types.RuleContext, _ types.RuleMsg, err error, _ string) {
				callbackErr = err
			}))
			if callbackErr != nil {
				t.Fatal(callbackErr)
			}
		}
		engine.OnMsgAndWait(input, types.WithStartNode("segment-source-switch"), types.WithOnEnd(func(_ types.RuleContext, msg types.RuleMsg, err error, _ string) {
			output, callbackErr = msg, err
		}))
		if callbackErr != nil {
			t.Fatal(callbackErr)
		}
		var resolve struct {
			Args []string `json:"args"`
		}
		if err := json.Unmarshal(output.GetBytes(), &resolve); err != nil {
			t.Fatal(err)
		}
		if len(resolve.Args) == 0 || resolve.Args[len(resolve.Args)-1] != "https://www.youtube.com/watch?v=Z4tHPyZBC8g" {
			t.Fatalf("cold lease cache (legacy=%v) did not resolve: %s", legacy, output.GetData())
		}
	}
	input = types.NewMsgWithJsonData(`{"revision":"` + revision + `","segments":[{"duration":2}]}`)
	input.Metadata.PutValue("videoId", "Z4tHPyZBC8g")
	input.Metadata.PutValue("source", string(sourceJSON))
	input.Metadata.PutValue("leaseExpiresAtMs", leaseExpiresAt)
	var cachedValue any
	var cacheErr error
	engine.OnMsgAndWait(input, types.WithStartNode("parent-acquire-request"), types.WithSkipTellNext(), types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, _ string) {
		output, callbackErr = msg, err
		cachedValue, cacheErr = ctx.ChainCache().Get("indexed-media:lease:Z4tHPyZBC8g:" + revision)
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
	if cached.Operation != "produce" || cached.ExpectedRevision != revision || cached.Segment != 1 || !reflect.DeepEqual(cached.Source, request.Source) {
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

func TestExampleManifestPublicOrigin(t *testing.T) {
	node := exampleScriptNodes(t, "manifest-public-origin")["manifest-public-origin"]
	dsl, err := json.Marshal(map[string]any{
		"ruleChain": map[string]any{"id": "manifest-public-origin-test", "root": true},
		"metadata": map[string]any{
			"firstNodeIndex": 0,
			"nodes": []json.RawMessage{
				node,
				json.RawMessage(`{"id":"end","type":"end","configuration":{}}`),
			},
			"connections": []map[string]string{{"fromId": "manifest-public-origin", "toId": "end", "type": types.Success}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pool := rulego.NewRuleGo()
	engine, err := pool.New("manifest-public-origin-test", dsl, rulego.WithConfig(rulego.NewConfig()))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(nil)

	manifest := "#EXTM3U\n/youtube/video/segments/revision/0.ts\n#EXT-X-ENDLIST\n"
	input := types.NewMsg(0, "", types.TEXT, types.NewMetadata(), manifest)
	var output types.RuleMsg
	var callbackErr error
	engine.OnMsgAndWait(input, types.WithOnEnd(func(_ types.RuleContext, msg types.RuleMsg, err error, _ string) {
		output, callbackErr = msg, err
	}))
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	want := "#EXTM3U\nhttp://localhost:9090/youtube/video/segments/revision/0.ts\n#EXT-X-ENDLIST\n"
	if got := string(output.GetBytes()); got != want {
		t.Fatalf("absolute manifest = %q, want %q", got, want)
	}
}

func TestExampleSignedURLLeaseExpiry(t *testing.T) {
	normalize := exampleScriptNodes(t, "normalize-source")["normalize-source"]
	dsl, err := json.Marshal(map[string]any{
		"ruleChain": map[string]any{"id": "signed-url-lease-expiry-test", "root": true},
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
	engine, err := pool.New("signed-url-lease-expiry-test", dsl, rulego.WithConfig(rulego.NewConfig()))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(nil)

	videoExpiry := time.Now().Add(2 * time.Hour).Unix()
	audioExpiry := videoExpiry + int64(time.Hour/time.Second)
	media, err := json.Marshal(map[string]any{
		"id": "Z4tHPyZBC8g",
		"requested_formats": []map[string]any{
			{"url": fmt.Sprintf("https://media.invalid/v?foo=1&expire=%d", videoExpiry), "ext": "mp4", "vcodec": "avc1.64002a", "acodec": "none"},
			{"url": fmt.Sprintf("https://media.invalid/a?expire=%d&bar=1", audioExpiry), "ext": "m4a", "vcodec": "none", "acodec": "mp4a.40.2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"exit_code": 0, "stdout": string(media)})
	if err != nil {
		t.Fatal(err)
	}
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
	ttl, err := time.ParseDuration(output.GetMetadata().GetValue("leaseCacheTtl"))
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 10*time.Minute || ttl > 2*time.Hour {
		t.Fatalf("derived lease ttl = %v", ttl)
	}
	expiresAt, err := strconv.ParseInt(output.GetMetadata().GetValue("leaseExpiresAtMs"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if want := videoExpiry*1000 - int64(5*time.Minute/time.Millisecond); expiresAt != want {
		t.Fatalf("lease expiry = %d, want %d", expiresAt, want)
	}
}

func TestExampleManifestCache(t *testing.T) {
	const (
		videoID  = "Z4tHPyZBC8g"
		manifest = "#EXTM3U\n#EXT-X-ENDLIST\n"
	)
	revision := strings.Repeat("a", 64)
	mappingKey := "indexed-media:manifest:youtube:" + videoID + ":avc-1080-m4a:indexed-ts-v2"
	leaseKey := "indexed-media:lease:" + videoID + ":" + revision
	nodes := exampleScriptNodes(t, "manifest-request", "manifest-cache-switch", "cached-manifest-response", "manifest-response", "cached-produce-request", "commit-request", "stale-switch", "refresh-request")

	t.Run("fast hit bypasses miss path", func(t *testing.T) {
		seedLease := json.RawMessage(`{"id":"seed-lease","type":"jsTransform","configuration":{"jsScript":"$ctx.ChainCache().Set(String(msg.key),String(msg.value),'1h');return {'msg':msg,'metadata':metadata,'msgType':msgType,'dataType':dataType};"}}`)
		poison := json.RawMessage(`{"id":"poison","type":"jsTransform","configuration":{"jsScript":"throw new Error('poison miss path reached');"}}`)
		end := json.RawMessage(`{"id":"end","type":"end","configuration":{}}`)
		dsl, err := json.Marshal(map[string]any{
			"ruleChain": map[string]any{"id": "manifest-cache-hit-test", "root": true},
			"metadata": map[string]any{
				"firstNodeIndex": 0,
				"nodes": []json.RawMessage{
					nodes["manifest-request"], nodes["manifest-cache-switch"], nodes["cached-manifest-response"], nodes["manifest-response"], nodes["stale-switch"], nodes["refresh-request"], seedLease, poison, end,
				},
				"connections": []map[string]string{
					{"fromId": "manifest-request", "toId": "manifest-cache-switch", "type": types.Success},
					{"fromId": "manifest-cache-switch", "toId": "cached-manifest-response", "type": "Cached"},
					{"fromId": "manifest-cache-switch", "toId": "poison", "type": "Resolve"},
					{"fromId": "cached-manifest-response", "toId": "end", "type": types.Success},
					{"fromId": "stale-switch", "toId": "refresh-request", "type": "Refresh"},
					{"fromId": "refresh-request", "toId": "end", "type": types.Success},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		pool := rulego.NewRuleGo()
		engine, err := pool.New("manifest-cache-hit-test", dsl, rulego.WithConfig(rulego.NewConfig()))
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Stop(nil)

		leaseValue := `{"sourceKey":"youtube:Z4tHPyZBC8g","video":{"url":"https://secret.invalid/video"}}`
		seedBody, _ := json.Marshal(map[string]any{"key": leaseKey, "value": leaseValue})
		var callbackErr error
		engine.OnMsgAndWait(types.NewMsgWithJsonData(string(seedBody)), types.WithStartNode("seed-lease"), types.WithSkipTellNext(), types.WithOnEnd(func(_ types.RuleContext, _ types.RuleMsg, err error, _ string) {
			callbackErr = err
		}))
		if callbackErr != nil {
			t.Fatal(callbackErr)
		}

		expiresAt := time.Now().Add(time.Hour).UnixMilli()
		publish := types.NewMsgWithJsonData(`{}`)
		publish.Metadata.PutValue("videoId", videoID)
		publish.Metadata.PutValue("revision", revision)
		publish.Metadata.PutValue("manifest", manifest)
		publish.Metadata.PutValue("manifestCacheKey", mappingKey)
		publish.Metadata.PutValue("leaseExpiresAtMs", strconv.FormatInt(expiresAt, 10))
		var mappingValue any
		var cacheErr error
		engine.OnMsgAndWait(publish, types.WithStartNode("manifest-response"), types.WithSkipTellNext(), types.WithOnEnd(func(ctx types.RuleContext, _ types.RuleMsg, err error, _ string) {
			callbackErr = err
			mappingValue, cacheErr = ctx.ChainCache().Get(mappingKey)
		}))
		if callbackErr != nil || cacheErr != nil {
			t.Fatalf("publish callbackErr=%v cacheErr=%v", callbackErr, cacheErr)
		}
		var mapping map[string]any
		if err := json.Unmarshal([]byte(mappingValue.(string)), &mapping); err != nil {
			t.Fatal(err)
		}
		if len(mapping) != 3 || mapping["manifest"] != manifest || mapping["revision"] != revision || int64(mapping["leaseExpiresAtMs"].(float64)) != expiresAt {
			t.Fatalf("manifest mapping = %#v", mapping)
		}
		if strings.Contains(mappingValue.(string), "secret.invalid") || strings.Contains(mappingValue.(string), "headers") {
			t.Fatalf("manifest mapping leaked access data: %s", mappingValue)
		}

		request := types.NewMsgWithJsonData(`{}`)
		request.Metadata.PutValue("videoId", videoID)
		var output types.RuleMsg
		engine.OnMsgAndWait(request, types.WithStartNode("manifest-request"), types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, _ string) {
			output, callbackErr = msg, err
			gotLease, getErr := ctx.ChainCache().Get(leaseKey)
			if getErr != nil || gotLease != leaseValue {
				t.Errorf("lease after manifest hit = %#v, err = %v", gotLease, getErr)
			}
		}))
		if callbackErr != nil {
			t.Fatal(callbackErr)
		}
		if got := string(output.GetBytes()); got != manifest {
			t.Fatalf("cached manifest = %q", got)
		}

		stale := types.NewMsgWithJsonData(`{"kind":"source_stale"}`)
		stale.Metadata.PutValue("videoId", videoID)
		stale.Metadata.PutValue("revision", revision)
		var mappingPresent, leasePresent bool
		engine.OnMsgAndWait(stale, types.WithStartNode("stale-switch"), types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, _ string) {
			output, callbackErr = msg, err
			mappingPresent = ctx.ChainCache().Has(mappingKey)
			leasePresent = ctx.ChainCache().Has(leaseKey)
		}))
		if callbackErr != nil || mappingPresent || leasePresent || output.Metadata.GetValue("phase") != "refresh" {
			t.Fatalf("stale refresh: mapping=%v lease=%v phase=%q err=%v", mappingPresent, leasePresent, output.Metadata.GetValue("phase"), callbackErr)
		}
	})

	for _, tc := range []struct {
		name          string
		expiresAt     int64
		withLease     bool
		withMapping   bool
		legacyMapping bool
		legacyLease   bool
	}{
		{name: "cold cache"},
		{name: "expired mapping", expiresAt: time.Now().Add(-time.Minute).UnixMilli(), withLease: true, withMapping: true},
		{name: "missing matching lease", expiresAt: time.Now().Add(time.Hour).UnixMilli(), withMapping: true},
		{name: "legacy manifest and lease", expiresAt: time.Now().Add(time.Hour).UnixMilli(), withMapping: true, withLease: true, legacyMapping: true, legacyLease: true},
		{name: "legacy manifest with current lease", expiresAt: time.Now().Add(time.Hour).UnixMilli(), withMapping: true, withLease: true, legacyMapping: true},
		{name: "current manifest with legacy lease", expiresAt: time.Now().Add(time.Hour).UnixMilli(), withMapping: true, withLease: true, legacyLease: true},
	} {
		t.Run(tc.name+" refuses hit", func(t *testing.T) {
			seed := json.RawMessage(`{"id":"seed","type":"jsTransform","configuration":{"jsScript":"var cache=$ctx.ChainCache();if(msg.withMapping)cache.Set(String(msg.mappingKey),JSON.stringify(msg.mapping),'1h');if(msg.withLease)cache.Set(String(msg.leaseKey),'lease','1h');return {'msg':msg,'metadata':metadata,'msgType':msgType,'dataType':dataType};"}}`)
			miss := json.RawMessage(`{"id":"miss","type":"jsTransform","configuration":{"jsScript":"return {'msg':{'miss':true},'metadata':metadata,'msgType':msgType,'dataType':'JSON'};"}}`)
			end := json.RawMessage(`{"id":"end","type":"end","configuration":{}}`)
			dsl, err := json.Marshal(map[string]any{
				"ruleChain": map[string]any{"id": "manifest-cache-miss-test-" + strings.ReplaceAll(tc.name, " ", "-"), "root": true},
				"metadata": map[string]any{
					"firstNodeIndex": 0,
					"nodes":          []json.RawMessage{nodes["manifest-request"], nodes["manifest-cache-switch"], seed, miss, end},
					"connections": []map[string]string{
						{"fromId": "manifest-request", "toId": "manifest-cache-switch", "type": types.Success},
						{"fromId": "manifest-cache-switch", "toId": "miss", "type": "Resolve"},
						{"fromId": "miss", "toId": "end", "type": types.Success},
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			pool := rulego.NewRuleGo()
			engineID := "manifest-cache-miss-test-" + strings.ReplaceAll(tc.name, " ", "-")
			engine, err := pool.New(engineID, dsl, rulego.WithConfig(rulego.NewConfig()))
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Stop(nil)

			seedMappingKey, seedLeaseKey := mappingKey, leaseKey
			if tc.legacyMapping {
				seedMappingKey = strings.Replace(mappingKey, "indexed-media:", "indexed-vod:", 1)
			}
			if tc.legacyLease {
				seedLeaseKey = strings.Replace(leaseKey, "indexed-media:", "indexed-vod:", 1)
			}
			seedBody, _ := json.Marshal(map[string]any{
				"mappingKey":  seedMappingKey,
				"mapping":     map[string]any{"manifest": manifest, "revision": revision, "leaseExpiresAtMs": tc.expiresAt},
				"withLease":   tc.withLease,
				"withMapping": tc.withMapping,
				"leaseKey":    seedLeaseKey,
			})
			var callbackErr error
			engine.OnMsgAndWait(types.NewMsgWithJsonData(string(seedBody)), types.WithStartNode("seed"), types.WithSkipTellNext(), types.WithOnEnd(func(_ types.RuleContext, _ types.RuleMsg, err error, _ string) {
				callbackErr = err
			}))
			if callbackErr != nil {
				t.Fatal(callbackErr)
			}
			request := types.NewMsgWithJsonData(`{}`)
			request.Metadata.PutValue("videoId", videoID)
			var output types.RuleMsg
			var mappingPresent bool
			engine.OnMsgAndWait(request, types.WithStartNode("manifest-request"), types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, _ string) {
				output, callbackErr = msg, err
				mappingPresent = ctx.ChainCache().Has(mappingKey)
				if tc.legacyMapping && !ctx.ChainCache().Has(seedMappingKey) {
					t.Error("legacy manifest was removed instead of ignored")
				}
				if tc.legacyLease && !ctx.ChainCache().Has(seedLeaseKey) {
					t.Error("legacy lease was removed instead of ignored")
				}
			}))
			if callbackErr != nil {
				t.Fatal(callbackErr)
			}
			var result struct {
				Miss bool `json:"miss"`
			}
			if err := json.Unmarshal(output.GetBytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Miss || mappingPresent {
				t.Fatalf("miss=%v mappingPresent=%v", result.Miss, mappingPresent)
			}
		})
	}

	t.Run("cached member does not restore source lease at commit", func(t *testing.T) {
		seedLease := json.RawMessage(`{"id":"seed-lease","type":"jsTransform","configuration":{"jsScript":"$ctx.ChainCache().Set(String(msg.key),String(msg.value),'1h');return {'msg':msg,'metadata':metadata,'msgType':msgType,'dataType':dataType};"}}`)
		deleteLease := json.RawMessage(`{"id":"delete-lease","type":"jsTransform","configuration":{"jsScript":"$ctx.ChainCache().Delete('indexed-media:lease:'+metadata.videoId+':'+metadata.revision);return {'msg':msg,'metadata':metadata,'msgType':msgType,'dataType':dataType};"}}`)
		end := json.RawMessage(`{"id":"end","type":"end","configuration":{}}`)
		dsl, err := json.Marshal(map[string]any{
			"ruleChain": map[string]any{"id": "cached-member-no-lease-extension-test", "root": true},
			"metadata": map[string]any{
				"firstNodeIndex": 0,
				"nodes":          []json.RawMessage{nodes["cached-produce-request"], deleteLease, nodes["commit-request"], seedLease, end},
				"connections": []map[string]string{
					{"fromId": "cached-produce-request", "toId": "delete-lease", "type": types.Success},
					{"fromId": "delete-lease", "toId": "commit-request", "type": types.Success},
					{"fromId": "commit-request", "toId": "end", "type": types.Success},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		pool := rulego.NewRuleGo()
		engine, err := pool.New("cached-member-no-lease-extension-test", dsl, rulego.WithConfig(rulego.NewConfig()))
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Stop(nil)

		leaseValue := `{"sourceKey":"youtube:Z4tHPyZBC8g","video":{},"audio":{}}`
		seedBody, _ := json.Marshal(map[string]any{"key": leaseKey, "value": leaseValue})
		var callbackErr error
		engine.OnMsgAndWait(types.NewMsgWithJsonData(string(seedBody)), types.WithStartNode("seed-lease"), types.WithSkipTellNext(), types.WithOnEnd(func(_ types.RuleContext, _ types.RuleMsg, err error, _ string) {
			callbackErr = err
		}))
		if callbackErr != nil {
			t.Fatal(callbackErr)
		}

		input := types.NewMsgWithJsonData(`{}`)
		input.Metadata.PutValue("videoId", videoID)
		input.Metadata.PutValue("revision", revision)
		input.Metadata.PutValue("segment", "3")
		input.Metadata.PutValue("stagingDir", "/tmp/generation")
		input.Metadata.PutValue("maxBytes", "1024")
		input.Metadata.PutValue("publishBy", "2026-08-29T18:00:00Z")
		input.Metadata.PutValue("resourceId", "resource")
		input.Metadata.PutValue("generation", "generation")
		input.Metadata.PutValue("entrypoint", "3.ts")
		var leasePresent bool
		engine.OnMsgAndWait(input, types.WithStartNode("cached-produce-request"), types.WithOnEnd(func(ctx types.RuleContext, _ types.RuleMsg, err error, _ string) {
			callbackErr = err
			leasePresent = ctx.ChainCache().Has(leaseKey)
		}))
		if callbackErr != nil {
			t.Fatal(callbackErr)
		}
		if leasePresent {
			t.Fatal("cached member commit restored the deleted source lease")
		}
	})
}

func exampleScriptNodes(t *testing.T, ids ...string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("examples/youtube-hls/chain.json")
	if err != nil {
		t.Fatal(err)
	}
	var example struct {
		Metadata struct {
			Nodes []json.RawMessage `json:"nodes"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &example); err != nil {
		t.Fatal(err)
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	found := make(map[string]json.RawMessage, len(ids))
	for _, node := range example.Metadata.Nodes {
		var identity struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(node, &identity); err != nil {
			t.Fatal(err)
		}
		if wanted[identity.ID] {
			found[identity.ID] = node
		}
	}
	for _, id := range ids {
		if found[id] == nil {
			t.Fatalf("example script node %q is missing", id)
		}
	}
	return found
}

func TestRuleGoSharedOwnerAndBorrower(t *testing.T) {
	if err := rulego.Registry.Register(&indexedMediaNode{}); err != nil {
		t.Fatal(err)
	}
	defer rulego.Registry.Unregister(componentType)
	dsl := fmt.Sprintf(`{
  "ruleChain":{"id":"indexed-media-test","root":true},
  "metadata":{
    "firstNodeIndex":1,
    "nodes":[
      {"id":"owner","type":"indexedMedia","configuration":{
        "root":%q,"ffmpegAddress":"127.0.0.1:1","ffmpegSecret":"test"}},
      {"id":"borrower","type":"indexedMedia","configuration":{"root":"ref://owner"}},
      {"id":"end","type":"end","configuration":{}}
    ],
    "connections":[
      {"fromId":"borrower","toId":"end","type":"Success"},
      {"fromId":"borrower","toId":"end","type":"Failure"}
    ]
  }
}`, t.TempDir())
	pool := rulego.NewRuleGo()
	engine, err := pool.New("indexed-media-test", []byte(dsl), rulego.WithConfig(rulego.NewConfig()))
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

type nodeResultContext struct {
	types.RuleContext
	output   types.RuleMsg
	relation string
}

func (*nodeResultContext) GetContext() context.Context { return context.Background() }
func (ctx *nodeResultContext) TellSuccess(msg types.RuleMsg) {
	ctx.output, ctx.relation = msg, types.Success
}
func (ctx *nodeResultContext) TellFailure(msg types.RuleMsg, _ error) {
	ctx.output, ctx.relation = msg, types.Failure
}

func TestNodeRequiresPairedTracks(t *testing.T) {
	server := rangeServer(t, nil)
	defer server.Close()
	root := t.TempDir()
	staging := filepath.Join(root, "generation")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	node := &indexedMediaNode{}
	if err := node.Init(rulego.NewConfig(), types.Configuration{
		"root": root, "ffmpegAddress": "127.0.0.1:1", "ffmpegSecret": "test",
	}); err != nil {
		t.Fatal(err)
	}
	defer node.Destroy()
	manager, err := node.GetSafely()
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("b", 64)
	source := testLease(server.URL+"/video", server.URL+"/audio")
	manager.inspectFn = func(_ context.Context, lease mediaLease) (*sourceBundle, error) {
		return testBundle(lease, revision), nil
	}
	manager.runFn = func(_ context.Context, _ ffmpegclient.Config, invocation ffmpegclient.Invocation, _ ffmpegclient.OutputFunc) (int, error) {
		return 0, os.WriteFile(invocation.Args[len(invocation.Args)-1], []byte("mpeg-ts"), 0o600)
	}
	for _, operation := range []string{"inspect", "produce"} {
		for _, tracks := range []string{"paired", "missing-video", "missing-audio"} {
			t.Run(operation+"/"+tracks, func(t *testing.T) {
				sourceBody, err := json.Marshal(source)
				if err != nil {
					t.Fatal(err)
				}
				var lease map[string]any
				if err := json.Unmarshal(sourceBody, &lease); err != nil {
					t.Fatal(err)
				}
				if tracks != "paired" {
					delete(lease, strings.TrimPrefix(tracks, "missing-"))
				}
				request := map[string]any{"operation": operation, "source": lease}
				if operation == "produce" {
					request["expectedRevision"] = revision
					request["segment"] = 0
					request["stagingDir"] = staging
					request["maxBytes"] = 1024
					request["publishBy"] = time.Now().Add(time.Hour)
				}
				body, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				ctx := &nodeResultContext{}
				node.OnMsg(ctx, types.NewMsgWithJsonData(string(body)))
				if tracks == "paired" {
					if ctx.relation != types.Success {
						t.Fatalf("paired request relation=%q output=%s", ctx.relation, ctx.output.GetData())
					}
				} else {
					var failure nodeError
					if err := json.Unmarshal(ctx.output.GetBytes(), &failure); err != nil {
						t.Fatal(err)
					}
					if ctx.relation != types.Failure || failure.Kind != "invalid_input" {
						t.Fatalf("relation=%q failure=%#v", ctx.relation, failure)
					}
				}
			})
		}
	}
}
