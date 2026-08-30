#!/usr/bin/env bash

set -Eeuo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
runtime_ref=$(jq -er .runtime "$root/plugin-abi-release.json")
ffoip_server_ref=ghcr.io/killbus/ffmpeg-over-ip-server@sha256:61eb8c18b031b01d4d5a3de8ccc1691314fccb03404d7c6192b436b97ad63427
ffoip_client_ref=ghcr.io/killbus/ffmpeg-over-ip-client@sha256:58b5061521d705e1bc808d487242759914541f330de3b33be98b73ce367d8f2a

: "${INDEXED_PLUGIN:?set INDEXED_PLUGIN to the candidate indexed-vod .so}"
: "${FFMPEG_PLUGIN:?set FFMPEG_PLUGIN to the verified ffmpeg-over-ip .so}"
: "${ORIGIN_PLUGIN:?set ORIGIN_PLUGIN to the verified resource-origin .so}"

for command in docker go curl jq sha256sum stat; do
	command -v "$command" >/dev/null || { printf 'missing command: %s\n' "$command" >&2; exit 1; }
done
for artifact in "$INDEXED_PLUGIN" "$FFMPEG_PLUGIN" "$ORIGIN_PLUGIN"; do
	test -f "$artifact" || { printf 'missing plugin: %s\n' "$artifact" >&2; exit 1; }
done

mkdir -p "$root/tmp"
run_dir=$(mktemp -d "$root/tmp/e2e-hls.XXXXXX")
chmod 0777 "$run_dir"
network="indexed-vod-e2e-${RANDOM}-${RANDOM}"
ffoip_container="ffoip-${RANDOM}-${RANDOM}"
fixture_container="ytdlp-${RANDOM}-${RANDOM}"
rulego_container="rulego-${RANDOM}-${RANDOM}"

dump_logs() {
	if test -n "${fixture_port:-}"; then
		printf '\n===== fixture stats =====\n' >&2
		curl --silent --show-error --max-time 2 "http://127.0.0.1:${fixture_port}/stats" >&2 || true
	fi
	if test -n "${rulego_url:-}"; then
		printf '\n===== RuleGo run logs =====\n' >&2
		curl --silent --show-error --max-time 2 \
			"$rulego_url/api/v1/logs/runs?chainId=youtube-indexed-hls&size=10&page=1" >&2 || true
	fi
	for container in "$rulego_container" "$fixture_container" "$ffoip_container"; do
		if docker inspect "$container" >/dev/null 2>&1; then
			printf '\n===== %s =====\n' "$container" >&2
			docker logs "$container" >&2 || true
		fi
	done
}

cleanup() {
	docker rm -f "$rulego_container" "$fixture_container" "$ffoip_container" >/dev/null 2>&1 || true
	docker network rm "$network" >/dev/null 2>&1 || true
}

failed() {
	status=$?
	dump_logs
	cleanup
	exit "$status"
}

trap failed ERR
trap cleanup EXIT

retry() {
	local attempt
	for attempt in 1 2 3 4; do
		if "$@"; then return 0; fi
		test "$attempt" -lt 4 || return 1
		sleep $((2 ** attempt))
	done
}

wait_http() {
	local url=$1
	local container=$2
	local attempt
	for attempt in $(seq 1 150); do
		if curl --fail --silent --show-error --connect-timeout 1 --max-time 2 "$url" >/dev/null; then return 0; fi
		if test "$(docker inspect --format='{{.State.Running}}' "$container")" != true; then return 1; fi
		sleep 0.2
	done
	return 1
}

assert_bounded_ranges() {
	local stats=$1
	local video_size=$2
	local audio_size=$3
	jq -e --argjson video_size "$video_size" --argjson audio_size "$audio_size" '
		def span:
			(.range | capture("^bytes=(?<start>[0-9]+)-(?<end>[0-9]+)$")) |
			((.end | tonumber) - (.start | tonumber) + 1);
		[.requests[] | select(.path == "/video.mp4" or .path == "/audio.m4a")] as $media |
		($media | length) > 0 and
		all($media[];
			(.range | test("^bytes=[0-9]+-[0-9]+$")) and
			.bytes >= 0 and .bytes <= span and
			span < (if .path == "/video.mp4" then $video_size else $audio_size end)
		)
	' "$stats" >/dev/null
}

docker network create "$network" >/dev/null
retry docker pull "$ffoip_server_ref" >/dev/null
retry docker pull "$ffoip_client_ref" >/dev/null
retry docker pull "$runtime_ref" >/dev/null

docker run --rm --detach --name "$ffoip_container" --network "$network" --network-alias ffoip \
	-e FFMPEG_OVER_IP_SERVER_ADDRESS=0.0.0.0:5050 \
	-e FFMPEG_OVER_IP_SERVER_AUTH_SECRET=fixture-secret \
	-e FFMPEG_OVER_IP_SERVER_LOG=stdout \
	"$ffoip_server_ref" >/dev/null

generate_fixture() {
	docker run --rm --network "$network" -v "$run_dir:/work" -w /work \
		-e FFMPEG_OVER_IP_CLIENT_ADDRESS=ffoip:5050 \
		-e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=fixture-secret \
		-e FFMPEG_OVER_IP_CLIENT_LOG=stderr \
		"$ffoip_client_ref" -hide_banner -loglevel error "$@"
}

retry generate_fixture -version >/dev/null
retry generate_fixture -y -f lavfi -i testsrc2=size=160x90:rate=30 -t 16 \
	-an -c:v libx264 -preset ultrafast -g 60 -keyint_min 60 -sc_threshold 0 \
	-movflags +dash+global_sidx -frag_duration 2000000 /work/video.mp4
retry generate_fixture -y -f lavfi -i sine=frequency=1000:sample_rate=48000 -t 16 \
	-vn -c:a aac -b:a 96k -movflags +dash+global_sidx -frag_duration 2000000 /work/audio.m4a
video_size=$(stat -c %s "$run_dir/video.mp4")
audio_size=$(stat -c %s "$run_dir/audio.m4a")

CGO_ENABLED=0 go build -trimpath -o "$run_dir/fixture-server" "$root/tests/e2e/fixture-server.go"
chmod 0755 "$run_dir/fixture-server"
docker run --rm --user 65532:65532 -v "$run_dir:/work" --entrypoint /work/fixture-server \
	"$runtime_ref" -dir /work -normalize-video-sap

docker run --rm --detach --name "$fixture_container" --network "$network" --network-alias ytdlp \
	-p 127.0.0.1::8080 -v "$run_dir:/work:ro" --entrypoint /work/fixture-server \
	"$runtime_ref" -listen :8080 -dir /work >/dev/null
fixture_port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "8080/tcp") 0).HostPort}}' "$fixture_container")
wait_http "http://127.0.0.1:${fixture_port}/health" "$fixture_container"

mkdir -p "$run_dir/data/plugins" "$run_dir/data/resource-origin"
cp "$INDEXED_PLUGIN" "$run_dir/data/plugins/01-indexed.so"
cp "$ORIGIN_PLUGIN" "$run_dir/data/plugins/02-origin.so"
cp "$FFMPEG_PLUGIN" "$run_dir/data/plugins/03-ffmpeg.so"
cp "$root/tests/e2e/config.conf" "$run_dir/config.conf"
cp "$root/tests/e2e/node-pool.json" "$run_dir/node_pool.json"
jq '
	.ruleChain.debugMode = true |
	.ruleChain.additionalInfo.runLogMode = "detail" |
	(.metadata.nodes[] | select(.id == "manifest-public-origin") | .configuration.jsScript) |=
		gsub("http://localhost:9090"; "http://rulego:9090") |
	(.metadata.nodes[] | select(.id == "parent-acquire-request" or .id == "segment-request") | .configuration.jsScript) |=
		gsub("21600000"; "(metadata.videoId===\"expiry01\"?3000:21600000)") |
	(.metadata.nodes[] | select(.id == "child-acquire-request") | .configuration.jsScript) |=
		gsub("600000"; "(metadata.videoId===\"expiry01\"?3000:600000)")
' \
	"$root/examples/youtube-hls/chain.json" >"$run_dir/chain.json"
chmod -R a+rwX "$run_dir/data"

start_rulego() {
	local config_path=${1:-$run_dir/config.conf}
	docker run --rm --detach --name "$rulego_container" --network "$network" --network-alias rulego \
		-p 127.0.0.1::9090 \
		-v "$config_path:/app/config.conf:ro" \
		-v "$run_dir/node_pool.json:/app/node_pool.json:ro" \
		-v "$run_dir/data:/app/data" \
		"$runtime_ref" >/dev/null
	rulego_port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "9090/tcp") 0).HostPort}}' "$rulego_container")
	rulego_url="http://127.0.0.1:${rulego_port}"
	wait_http "$rulego_url/api/v1/components" "$rulego_container"
}

start_rulego
curl --fail --silent --show-error "$rulego_url/api/v1/components" >"$run_dir/components.json"
jq -e '
	(.nodes | any(.type == "ffmpegOverIp")) and
	(.nodes | any(.type == "ffmpegOverIpProducer")) and
	(.nodes | any(.type == "indexedVod")) and
	(.nodes | any(.type == "resourceOrigin")) and
	(.builtins.endpoints.outProcessors | index("ffmpegOverIpResponse") != null) and
	(.builtins.endpoints.outProcessors | index("resourceOriginResponse") != null)
' "$run_dir/components.json" >/dev/null
docker logs "$rulego_container" >"$run_dir/rulego-startup.log" 2>&1
if grep -Eiq 'plugin was built with a different version of package|failed to load plugin|load plugin.*(error|failed)' \
	"$run_dir/rulego-startup.log"; then
	exit 1
fi

curl --fail --silent --show-error -H 'Content-Type: application/json' \
	--data-binary @"$run_dir/chain.json" \
	"$rulego_url/api/v1/rules/youtube-indexed-hls" >/dev/null

manifest_url="$rulego_url/youtube/fixture01/index.m3u8"
retry curl --fail --silent --show-error --max-time 180 -D "$run_dir/index.headers" \
	-o "$run_dir/index.m3u8" "$manifest_url"
grep -Eq '^HTTP/[^ ]+ 200 ' "$run_dir/index.headers"
grep -Fxq '#EXT-X-PLAYLIST-TYPE:VOD' "$run_dir/index.m3u8"
grep -Fxq '#EXT-X-ENDLIST' "$run_dir/index.m3u8"
if grep -Eq 'fixture-lease|fixture-secret' "$run_dir/index.m3u8"; then
	exit 1
fi
public_origin=http://rulego:9090
awk -v prefix="$public_origin/youtube/" 'NF && $0 !~ /^#/ && index($0,prefix) != 1 { exit 1 }' "$run_dir/index.m3u8"
mapfile -t members < <(grep "^${public_origin}/youtube/" "$run_dir/index.m3u8")
test "${#members[@]}" -ge 8
target_index=$(( ${#members[@]} * 3 / 4 ))
target=${members[$target_index]}
target_path=${target#"$public_origin"}

curl --fail --silent --show-error "http://127.0.0.1:${fixture_port}/stats" >"$run_dir/stats-before.json"
before_video=$(jq '[.requests[] | select(.path == "/video.mp4" and .status == 206 and (.range | startswith("bytes=0-") | not))] | length' "$run_dir/stats-before.json")
curl --fail --silent --show-error --location --max-time 180 "$rulego_url$target_path" >"$run_dir/member-a.ts" &
first_pid=$!
curl --fail --silent --show-error --location --max-time 180 "$rulego_url$target_path" >"$run_dir/member-b.ts" &
second_pid=$!
wait "$first_pid"
wait "$second_pid"
cmp "$run_dir/member-a.ts" "$run_dir/member-b.ts"

curl --fail --silent --show-error "http://127.0.0.1:${fixture_port}/stats" >"$run_dir/stats-after.json"
after_video=$(jq '[.requests[] | select(.path == "/video.mp4" and .status == 206 and (.range | startswith("bytes=0-") | not))] | length' "$run_dir/stats-after.json")
test "$((after_video - before_video))" -eq 2
jq -e '
	(.faults | index("video.mp4:index") != null) and
	(.faults | index("audio.m4a:index") != null) and
	(.faults | index("video.mp4:distant") != null) and
	(.faults | index("audio.m4a:distant") != null)
' "$run_dir/stats-after.json" >/dev/null
assert_bounded_ranges "$run_dir/stats-after.json" "$video_size" "$audio_size"
jq -e '
	def span:
		(.range | capture("^bytes=(?<start>[0-9]+)-(?<end>[0-9]+)$")) |
		((.end | tonumber) - (.start | tonumber) + 1);
	.requests as $requests |
	[$requests[] as $failed |
		select(
			$failed.status == 429 or $failed.status == 503 or $failed.status == 0 or
			($failed.status == 206 and $failed.bytes < ($failed | span))
		) |
		[$requests[] | select(.path == $failed.path and .range == $failed.range)] | length
	] as $attempts |
	($attempts | length) == 4 and all($attempts[]; . == 2)
' "$run_dir/stats-after.json" >/dev/null

curl --fail --silent --show-error --max-time 30 -D "$run_dir/member-before-restart.headers" \
	-o /dev/null "$rulego_url$target_path"
grep -Eq '^HTTP/[^ ]+ 307 ' "$run_dir/member-before-restart.headers"
static_location_before=$(awk 'BEGIN{IGNORECASE=1}/^Location:/{sub(/\r$/,"",$2);print $2}' "$run_dir/member-before-restart.headers")
test "${static_location_before#/resources/}" != "$static_location_before"

docker run --rm --network "$network" \
	-e FFMPEG_OVER_IP_CLIENT_ADDRESS=ffoip:5050 \
	-e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=fixture-secret \
	-e FFMPEG_OVER_IP_CLIENT_LOG=stderr \
	"$ffoip_client_ref" -hide_banner -loglevel error -ss 12 \
	-i http://rulego:9090/youtube/fixture01/index.m3u8 -t 1 \
	-map 0:v:0 -map 0:a:0 -f null -

manifest_sha=$(sha256sum "$run_dir/index.m3u8" | awk '{print $1}')
member_sha=$(sha256sum "$run_dir/member-a.ts" | awk '{print $1}')
curl --fail --silent --show-error "http://127.0.0.1:${fixture_port}/stats" >"$run_dir/stats-before-restart.json"
video_media_requests=$(jq '[.requests[] | select(.path == "/video.mp4" and (.range | startswith("bytes=0-") | not))] | length' "$run_dir/stats-before-restart.json")
docker rm -f "$rulego_container" >/dev/null
start_rulego
manifest_url="$rulego_url/youtube/fixture01/index.m3u8"
retry curl --fail --silent --show-error --max-time 180 \
	-o "$run_dir/index-after-restart.m3u8" "$manifest_url"
curl --fail --silent --show-error --location --max-time 180 "$rulego_url$target_path" >"$run_dir/member-after-restart.ts"
test "$(sha256sum "$run_dir/index-after-restart.m3u8" | awk '{print $1}')" = "$manifest_sha"
test "$(sha256sum "$run_dir/member-after-restart.ts" | awk '{print $1}')" = "$member_sha"
curl --fail --silent --show-error "http://127.0.0.1:${fixture_port}/stats" >"$run_dir/stats-restart.json"
test "$(jq '[.requests[] | select(.path == "/video.mp4" and (.range | startswith("bytes=0-") | not))] | length' "$run_dir/stats-restart.json")" -eq "$video_media_requests"

curl --fail --silent --show-error --max-time 30 -D "$run_dir/member.headers" \
	-o /dev/null "$rulego_url$target_path"
grep -Eq '^HTTP/[^ ]+ 307 ' "$run_dir/member.headers"
static_location=$(awk 'BEGIN{IGNORECASE=1}/^Location:/{sub(/\r$/,"",$2);print $2}' "$run_dir/member.headers")
test "${static_location#/resources/}" != "$static_location"
test "$static_location" = "$static_location_before"
grep -Eiq '^Access-Control-Allow-Origin:[[:space:]]*\*[[:space:]]*$' "$run_dir/index.headers"
grep -Eiq '^Access-Control-Allow-Origin:[[:space:]]*\*[[:space:]]*$' "$run_dir/member.headers"
curl --fail --silent --show-error -H 'Range: bytes=0-15' -D "$run_dir/range.headers" \
	-o "$run_dir/range.bin" "$rulego_url$static_location"
grep -Eq '^HTTP/[^ ]+ 206 ' "$run_dir/range.headers"
grep -Eiq '^Content-Range: bytes 0-15/' "$run_dir/range.headers"
test "$(wc -c < "$run_dir/range.bin")" -eq 16
member_size=$(stat -c %s "$run_dir/member-after-restart.ts")
invalid_range_status=$(curl --silent --show-error -H 'Range: bytes=999999999-' \
	-D "$run_dir/invalid-range.headers" -o /dev/null -w '%{http_code}' "$rulego_url$static_location")
test "$invalid_range_status" = 416
grep -Eiq "^Content-Range: bytes \*/${member_size}[[:space:]]*$" "$run_dir/invalid-range.headers"
last_modified=$(awk 'BEGIN{IGNORECASE=1}/^Last-Modified:/{sub(/^Last-Modified:[[:space:]]*/,"");sub(/\r$/,"");print}' "$run_dir/range.headers")
test -n "$last_modified"
test "$(curl --silent --show-error -H "If-Modified-Since: $last_modified" -o /dev/null -w '%{http_code}' "$rulego_url$static_location")" = 304

expiry_manifest_url="$rulego_url/youtube/expiry01/index.m3u8"
retry curl --fail --silent --show-error --max-time 180 \
	-o "$run_dir/expiry-index.m3u8" "$expiry_manifest_url"
mapfile -t expiry_members < <(grep "^${public_origin}/youtube/" "$run_dir/expiry-index.m3u8")
test "${#expiry_members[@]}" -ge 8
expiry_target=${expiry_members[$(( ${#expiry_members[@]} * 3 / 4 ))]}
expiry_target_path=${expiry_target#"$public_origin"}
curl --fail --silent --show-error --max-time 180 -D "$run_dir/expiry-member.headers" \
	-o /dev/null "$rulego_url$expiry_target_path"
grep -Eq '^HTTP/[^ ]+ 307 ' "$run_dir/expiry-member.headers"
expiry_static_location=$(awk 'BEGIN{IGNORECASE=1}/^Location:/{sub(/\r$/,"",$2);print $2}' "$run_dir/expiry-member.headers")
test "${expiry_static_location#/resources/}" != "$expiry_static_location"
curl --fail --silent --show-error "$rulego_url$expiry_static_location" >"$run_dir/expiry-member.ts"
curl --fail --silent --show-error "http://127.0.0.1:${fixture_port}/stats" >"$run_dir/stats-expiry-ready.json"
expiry_media_requests=$(jq '[.requests[] | select(.path == "/video.mp4" or .path == "/audio.m4a")] | length' "$run_dir/stats-expiry-ready.json")

expiry_deadline=$((SECONDS + 15))
while true; do
	expiry_status=$(curl --silent --show-error -o "$run_dir/expiry-response.json" -w '%{http_code}' "$rulego_url$expiry_target_path")
	if test "$expiry_status" = 502; then
		break
	fi
	test "$expiry_status" = 307
	test "$SECONDS" -lt "$expiry_deadline"
	sleep 0.2
done
jq -e '.state == "failed" and .failureKind == "parent_unavailable"' "$run_dir/expiry-response.json" >/dev/null
test "$(curl --silent --show-error -o /dev/null -w '%{http_code}' "$rulego_url$expiry_static_location")" = 404
curl --fail --silent --show-error "http://127.0.0.1:${fixture_port}/stats" >"$run_dir/stats-expired.json"
test "$(jq '[.requests[] | select(.path == "/video.mp4" or .path == "/audio.m4a")] | length' "$run_dir/stats-expired.json")" -eq "$expiry_media_requests"
assert_bounded_ranges "$run_dir/stats-expired.json" "$video_size" "$audio_size"

sed 's#^resource_mapping = /resources/#resource_mapping = /broken/#' \
	"$run_dir/config.conf" >"$run_dir/broken-config.conf"
docker rm -f "$rulego_container" >/dev/null
start_rulego "$run_dir/broken-config.conf"
curl --fail --silent --show-error --max-time 30 -D "$run_dir/broken-member.headers" \
	-o /dev/null "$rulego_url$target_path"
grep -Eq '^HTTP/[^ ]+ 307 ' "$run_dir/broken-member.headers"
broken_static_location=$(awk 'BEGIN{IGNORECASE=1}/^Location:/{sub(/\r$/,"",$2);print $2}' "$run_dir/broken-member.headers")
test "$broken_static_location" = "$static_location"
test "$(curl --silent --show-error -o /dev/null -w '%{http_code}' "$rulego_url$broken_static_location")" = 404

printf 'hermetic HLS seek passed: members=%d target=%s manifest=%s member=%s\n' \
	"${#members[@]}" "$target" "$manifest_sha" "$member_sha"
