package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

// Keep the shipped connections, replacing only external operations. This catches
// a correct guard being bypassed by an incorrect edge in the example graph.
func exampleLifecycleGraph(t *testing.T, ids []string, replacements map[string]string) types.RuleEngine {
	t.Helper()
	raw, err := os.ReadFile("examples/youtube-hls/chain.json")
	if err != nil {
		t.Fatal(err)
	}
	var example struct {
		Metadata struct {
			Nodes       []json.RawMessage   `json:"nodes"`
			Connections []map[string]string `json:"connections"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &example); err != nil {
		t.Fatal(err)
	}
	wanted := make(map[string]bool)
	for _, id := range ids {
		wanted[id] = true
	}
	var nodes []json.RawMessage
	for _, node := range example.Metadata.Nodes {
		var identity struct{ ID string }
		if err := json.Unmarshal(node, &identity); err != nil {
			t.Fatal(err)
		}
		if !wanted[identity.ID] {
			continue
		}
		if script, ok := replacements[identity.ID]; ok {
			node, err = json.Marshal(map[string]any{"id": identity.ID, "type": "jsTransform", "configuration": map[string]any{"jsScript": script}})
			if err != nil {
				t.Fatal(err)
			}
		}
		nodes = append(nodes, node)
	}
	if len(nodes) != len(wanted) {
		t.Fatal("requested example nodes are missing")
	}
	var connections []map[string]string
	for _, edge := range example.Metadata.Connections {
		if wanted[edge["fromId"]] && wanted[edge["toId"]] {
			connections = append(connections, edge)
		}
	}
	dsl, err := json.Marshal(map[string]any{
		"ruleChain": map[string]any{"id": "lifecycle-test", "root": true},
		"metadata":  map[string]any{"firstNodeIndex": 0, "nodes": nodes, "connections": connections},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := rulego.NewRuleGo().New("lifecycle-test", dsl, rulego.WithConfig(rulego.NewConfig()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Stop(nil) })
	return engine
}

func TestExamplePublicationRequiresReadyParent(t *testing.T) {
	parent := strings.Repeat("b", 64)
	for _, start := range []string{"parent-origin", "commit-switch", "production-fail"} {
		for _, state := range []string{"ready", "failed", "expired", "pending", "not_found", ""} {
			t.Run(start+"/"+state, func(t *testing.T) {
				engine := exampleLifecycleGraph(t, []string{"parent-origin", "commit-switch", "manifest-response", "manifest-public-origin", "production-fail", "production-fail-response"}, map[string]string{
					"parent-origin":   "return {msg:msg,metadata:metadata,msgType:msgType,dataType:dataType};",
					"production-fail": "return {msg:msg,metadata:metadata,msgType:msgType,dataType:dataType};",
				})
				body, _ := json.Marshal(map[string]string{"state": state, "resourceId": parent, "failureKind": "production_timeout"})
				input := types.NewMsgWithJsonData(string(body))
				input.Metadata.PutValue("kind", "manifest")
				input.Metadata.PutValue("videoId", "fixture01")
				input.Metadata.PutValue("revision", strings.Repeat("a", 64))
				input.Metadata.PutValue("manifest", "#EXTM3U\n/youtube/fixture01/segments/revision/0.ts\n#EXT-X-ENDLIST\n")
				// Use an epoch value without depending on a source resolver.
				expiry, _ := json.Marshal(time.Now().Add(time.Hour).UnixMilli())
				input.Metadata.PutValue("leaseExpiresAtMs", string(expiry))
				var output types.RuleMsg
				var gotErr error
				var cached bool
				engine.OnMsgAndWait(input, types.WithStartNode(start), types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, _ string) {
					output, gotErr = msg, err
					cached = ctx.ChainCache().Has("indexed-media:manifest:youtube:fixture01:avc-1080-m4a:indexed-ts-v2")
				}))
				if state == "ready" && start != "production-fail" {
					if gotErr != nil || !cached || !strings.Contains(output.GetData(), "0.ts?parentResourceId="+parent+"\n") {
						t.Fatalf("ready publication: cached=%v output=%s err=%v", cached, output.GetData(), gotErr)
					}
				} else if gotErr == nil || cached || !strings.Contains(gotErr.Error(), "production_timeout") {
					t.Fatalf("unready publication: cached=%v err=%v", cached, gotErr)
				}
			})
		}
	}
}

func TestExampleCachedManifestChecksParent(t *testing.T) {
	for _, state := range []string{"ready", "failed", "expired", "pending", "not_found"} {
		t.Run(state, func(t *testing.T) {
			engine := exampleLifecycleGraph(t, []string{
				"manifest-response", "manifest-request", "manifest-cache-switch", "cached-parent-request", "cached-parent-lookup",
				"cached-parent-switch", "cached-manifest-response", "manifest-public-origin", "source-resolve-request",
			}, map[string]string{
				"cached-parent-lookup":   "if(msg.operation!=='resolve'||msg.member!=='0.ts'||msg.resourceId!==metadata.parentResourceId)throw new Error('invalid parent lookup');metadata.parentChecked='true';metadata.resourceOriginState=metadata.testState;metadata.resourceOriginUrl='/stale-location';return {msg:{state:metadata.testState,resourceId:msg.resourceId},metadata:metadata,msgType:msgType,dataType:'JSON'};",
				"source-resolve-request": "return {msg:{resolve:true,parentChecked:metadata.parentChecked},metadata:metadata,msgType:msgType,dataType:'JSON'};",
			})
			const key = "indexed-media:manifest:youtube:fixture01:avc-1080-m4a:indexed-ts-v2"
			parent, revision := strings.Repeat("b", 64), strings.Repeat("a", 64)
			// Seed via the real writer and then request the playlist in a new traversal.
			input := types.NewMsgWithJsonData(`{"state":"ready","resourceId":"` + parent + `"}`)
			input.Metadata.PutValue("videoId", "fixture01")
			input.Metadata.PutValue("revision", revision)
			input.Metadata.PutValue("manifest", "#EXTM3U\n/0.ts\n")
			expiry, _ := json.Marshal(time.Now().Add(time.Hour).UnixMilli())
			input.Metadata.PutValue("leaseExpiresAtMs", string(expiry))
			var seeded any
			engine.OnMsgAndWait(input, types.WithStartNode("manifest-response"), types.WithSkipTellNext(), types.WithOnEnd(func(ctx types.RuleContext, _ types.RuleMsg, err error, _ string) {
				if err != nil {
					t.Fatal(err)
				}
				if err := ctx.ChainCache().Set("indexed-media:lease:fixture01:"+revision, "lease", "1h"); err != nil {
					t.Fatal(err)
				}
				seeded, err = ctx.ChainCache().Get(key)
				if err != nil {
					t.Fatal(err)
				}
			}))
			request := types.NewMsgWithJsonData(`{}`)
			request.Metadata.PutValue("videoId", "fixture01")
			request.Metadata.PutValue("testState", state)
			var output types.RuleMsg
			var gotErr error
			engine.OnMsgAndWait(request, types.WithStartNode("manifest-request"), types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, _ string) {
				output, gotErr = msg, err
				current, cacheErr := ctx.ChainCache().Get(key)
				if cacheErr != nil || current != seeded {
					t.Errorf("lookup changed the shared manifest mapping: %v", cacheErr)
				}
			}))
			if gotErr != nil {
				t.Fatal(gotErr)
			}
			if state == "ready" {
				if !strings.HasPrefix(output.GetData(), "#EXTM3U\n") || !strings.Contains(output.GetData(), "?parentResourceId="+parent) {
					t.Fatalf("ready cached response: %s", output.GetData())
				}
			} else {
				var result struct {
					Resolve       bool   `json:"resolve"`
					ParentChecked string `json:"parentChecked"`
				}
				if err := json.Unmarshal(output.GetBytes(), &result); err != nil || !result.Resolve || result.ParentChecked != "true" {
					t.Fatalf("unready parent did not resolve afresh: %s, %v", output.GetData(), err)
				}
			}
		})
	}
}

func TestExampleSegmentReadsNeverReserveParents(t *testing.T) {
	for _, state := range []string{"ready", "failed", "expired", "pending", "not_found", "legacy"} {
		for _, segment := range []string{"0", "3"} {
			t.Run(state+"/"+segment, func(t *testing.T) {
				engine := exampleLifecycleGraph(t, []string{
					"segment-request", "segment-parent-switch", "parent-lookup", "parent-ready-switch",
					"initial-resolve-request", "child-acquire-request", "parent-unavailable",
				}, map[string]string{
					"parent-lookup": "if(msg.operation!=='resolve'||msg.key||msg.generation)throw new Error('segment reserved a parent');metadata.parentChecked='true';metadata.resourceOriginState=metadata.testState;metadata.resourceOriginUrl='/stale-location';return {msg:{state:metadata.testState,resourceId:msg.resourceId},metadata:metadata,msgType:msgType,dataType:'JSON'};",
				})
				input := types.NewMsgWithJsonData(`{}`)
				input.Metadata.PutValue("videoId", "fixture01")
				input.Metadata.PutValue("revision", strings.Repeat("a", 64))
				input.Metadata.PutValue("segment", segment+".ts")
				input.Metadata.PutValue("testState", state)
				parent := strings.Repeat("b", 64)
				if state != "legacy" {
					input.Metadata.PutValue("parentResourceId", parent)
				}
				var output types.RuleMsg
				var gotErr error
				engine.OnMsgAndWait(input, types.WithStartNode("segment-request"), types.WithOnEnd(func(_ types.RuleContext, msg types.RuleMsg, err error, _ string) {
					output, gotErr = msg, err
				}))
				if gotErr != nil {
					t.Fatal(gotErr)
				}
				var result map[string]any
				if err := json.Unmarshal(output.GetBytes(), &result); err != nil {
					t.Fatal(err)
				}
				if state == "ready" {
					if output.Metadata.GetValue("parentChecked") != "true" {
						t.Fatal("ready segment bypassed parent lookup")
					}
					if segment == "0" {
						if result["operation"] != "resolve" || result["resourceId"] != parent || result["member"] != "0.ts" {
							t.Fatalf("initial request: %#v", result)
						}
					} else if result["operation"] != "acquire" || result["parentResourceId"] != parent || !strings.HasSuffix(result["key"].(string), ":segment:3") {
						t.Fatalf("child request: %#v", result)
					}
				} else if result["state"] != "failed" || result["failureKind"] != "parent_unavailable" || result["operation"] != nil {
					t.Fatalf("unavailable parent: %#v", result)
				}
				if state != "ready" && (output.Metadata.GetValue("resourceOriginState") != "failed" || output.Metadata.GetValue("resourceOriginUrl") != "") {
					t.Fatalf("unavailable HTTP projection metadata: %#v", output.Metadata)
				}
			})
		}
	}
}
