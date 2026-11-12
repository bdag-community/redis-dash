package main

const bdagRedisFunctionLibrary = `#!lua name=bdag

local function json_decode(raw)
  if not raw or raw == "" then
    return {}
  end
  return cjson.decode(raw)
end

local function json_encode(value)
  return cjson.encode(value)
end

local function as_number(value)
  if value == nil or value == cjson.null or value == "" then
    return 0
  end
  return tonumber(value) or 0
end

local function as_string(value)
  if value == nil or value == cjson.null then
    return ""
  end
  return tostring(value)
end

local function as_bool(value, fallback)
  if value == nil or value == cjson.null then
    return fallback
  end
  if value == true then
    return true
  end
  if value == false then
    return false
  end
  local text = string.lower(tostring(value))
  if text == "false" or text == "0" or text == "no" then
    return false
  end
  if text == "true" or text == "1" or text == "yes" then
    return true
  end
  return fallback
end

local function short_address(address)
  address = as_string(address)
  if string.len(address) <= 12 then
    return address
  end
  return string.sub(address, 1, 6) .. "..." .. string.sub(address, -4)
end

local function member_for(height, hash)
  return tostring(height) .. "|" .. as_string(hash)
end

local function height_from_member(member)
  local sep = string.find(member, "|", 1, true)
  if not sep then
    return tonumber(member) or 0
  end
  return tonumber(string.sub(member, 1, sep - 1)) or 0
end

local function dec_count(key, field, delta)
  if field == "" then
    return
  end
  local next_value = redis.call("HINCRBY", key, field, -delta)
  if tonumber(next_value) <= 0 then
    redis.call("HDEL", key, field)
  end
end

local function snapshot_envelope(status, updated_at, stale_after, source, reasons, data)
  return {
    status = status,
    updated_at = updated_at,
    age_seconds = 0,
    stale_after_seconds = stale_after,
    degraded_reasons = reasons or {},
    source = source,
    data_version = 1,
    data = data or {},
  }
end

local function dashboard_event(event_type, updated_at, fields)
  fields = fields or {}
  fields.type = event_type
  if updated_at ~= "" then
    fields.updated_at = updated_at
  end
  local args = {"MAXLEN", "~", 1000, "*"}
  for key, value in pairs(fields) do
    table.insert(args, key)
    table.insert(args, as_string(value))
  end
  redis.call("XADD", "bdag:dashboard:events", unpack(args))
end

local function append_global_chart_point(snapshot, clusters)
  if as_string(snapshot.status) ~= "ok" or not as_bool(snapshot.window_complete, false) then
    return {seq = ""}
  end
  local counts = {}
  local labels = {}
  local local_pools = {}
  for _, item in ipairs(clusters or {}) do
    local address = string.lower(as_string(item.address))
    if address ~= "" then
      counts[address] = as_number(item.blocks)
      labels[address] = as_string(item.pool_label)
      if labels[address] == "" then
        labels[address] = as_string(item.label)
      end
      if labels[address] == "" then
        labels[address] = as_string(item.address_short)
      end
      if item.local_pool then
        local_pools[address] = true
      end
    end
  end
  local seq = redis.call("INCR", "bdag:global:chart_seq")
  local point = {
    seq = seq,
    t = as_string(snapshot.updated_at),
    height = as_number(snapshot.latest_block),
    window_blocks = as_number(snapshot.fetched_blocks),
    window_seconds = as_number(snapshot.scan_window_seconds),
    scan_window_hours = as_string(snapshot.scan_window_hours),
    counts = counts,
    labels = labels,
    local_pools = local_pools,
  }
  local encoded = json_encode(point)
  redis.call("RPUSH", "bdag:global:chart_history", encoded)
  redis.call("LTRIM", "bdag:global:chart_history", -744, -1)
  redis.call("SET", "bdag:global:chart_latest", encoded)
  redis.call("DEL", "bdag:global:history")
  return point
end

local trend_resolutions = {
  {seconds = 10, retention = 2 * 3600},
  {seconds = 30, retention = 6 * 3600},
  {seconds = 60, retention = 30 * 3600},
  {seconds = 300, retention = 4 * 24 * 3600},
  {seconds = 900, retention = 8 * 24 * 3600},
  {seconds = 3600, retention = 35 * 24 * 3600},
}

local function trend_index_key(seconds)
  return "bdag:trend:v2:index:" .. tostring(seconds)
end

local function trend_bucket_key(seconds, bucket_start)
  return "bdag:trend:v2:bucket:" .. tostring(seconds) .. ":" .. tostring(bucket_start)
end

local function update_trend_bucket(header, resolution)
  local timestamp_epoch = as_number(header.timestamp_epoch)
  local height = as_number(header.height)
  if timestamp_epoch <= 0 or height <= 0 then
    return
  end
  local producer = string.lower(as_string(header.producer))
  if producer == "" then
    producer = "unknown"
  end
  local bucket_start = math.floor(timestamp_epoch / resolution.seconds) * resolution.seconds
  local bucket = trend_bucket_key(resolution.seconds, bucket_start)
  redis.call("HSET", bucket,
    "bucket_start", tostring(bucket_start),
    "bucket_seconds", tostring(resolution.seconds),
    "updated_at", as_string(header.received_at),
    "updated_at_epoch", tostring(as_number(header.received_at_epoch)))
  redis.call("HSETNX", bucket, "first_height", tostring(height))
  redis.call("HSETNX", bucket, "first_timestamp_epoch", tostring(timestamp_epoch))
  redis.call("HINCRBY", bucket, "block_count", 1)
  redis.call("HINCRBY", bucket, "producer:" .. producer, 1)
  if as_bool(header.local_pool, false) then
    redis.call("HSET", bucket, "local:" .. producer, "1")
  end
  local previous_last = as_number(redis.call("HGET", bucket, "last_height"))
  if height >= previous_last then
    redis.call("HSET", bucket,
      "last_height", tostring(height),
      "last_hash", as_string(header.hash),
      "last_timestamp_epoch", tostring(timestamp_epoch))
  end
  redis.call("ZADD", trend_index_key(resolution.seconds), bucket_start, tostring(bucket_start))
end

local function trim_trend_resolution(resolution, latest_timestamp_epoch)
  if latest_timestamp_epoch <= 0 then
    return
  end
  local cutoff = latest_timestamp_epoch - resolution.retention
  local index_key = trend_index_key(resolution.seconds)
  local old = redis.call("ZRANGEBYSCORE", index_key, "-inf", tostring(cutoff))
  for _, bucket_start in ipairs(old) do
    redis.call("DEL", trend_bucket_key(resolution.seconds, bucket_start))
  end
  if #old > 0 then
    redis.call("ZREMRANGEBYSCORE", index_key, "-inf", tostring(cutoff))
  end
end

local function trend_resolution_for(seconds)
  seconds = as_number(seconds)
  for _, resolution in ipairs(trend_resolutions) do
    if as_number(resolution.seconds) == seconds then
      return resolution
    end
  end
  return nil
end

redis.register_function("bdag_apply_trend_sample", function(keys, args)
  local payload = json_decode(args[1])
  local headers = payload.headers or {}
  if #headers <= 0 then
    return json_encode({status = "ignored", reason = "empty headers"})
  end

  local sample_id = as_string(payload.sample_id)
  local done_key = as_string(payload.done_key)
  if done_key == "" then
    done_key = "bdag:trend:v2:sparse_done"
  end
  if sample_id ~= "" and redis.call("SISMEMBER", done_key, sample_id) == 1 then
    return json_encode({status = "duplicate", reason = "sample already applied"})
  end

  local state_key = as_string(payload.state_key)
  if state_key == "" then
    state_key = "bdag:trend:v2:backfill_state"
  end
  local state = json_decode(redis.call("GET", state_key))
  local resolutions = payload.resolutions or {3600}
  local applied = 0
  local latest_height = 0
  local latest_timestamp_epoch = 0
  local latest_timestamp = ""

  for _, header in ipairs(headers) do
    local height = as_number(header.height)
    if height > 0 then
      for _, seconds in ipairs(resolutions) do
        local resolution = trend_resolution_for(seconds)
        if resolution ~= nil then
          update_trend_bucket(header, resolution)
        end
      end
      if height > latest_height then
        latest_height = height
        latest_timestamp_epoch = as_number(header.timestamp_epoch)
        latest_timestamp = as_string(header.timestamp)
      end
      applied = applied + 1
    end
  end

  local reference_epoch = as_number(payload.reference_timestamp_epoch)
  if reference_epoch <= 0 then
    reference_epoch = latest_timestamp_epoch
  end
  for _, seconds in ipairs(resolutions) do
    local resolution = trend_resolution_for(seconds)
    if resolution ~= nil then
      trim_trend_resolution(resolution, reference_epoch)
    end
  end

  if sample_id ~= "" then
    redis.call("SADD", done_key, sample_id)
  end

  state.status = "ok"
  state.backfill_status = as_string(payload.backfill_status)
  if state.backfill_status == "" then
    state.backfill_status = "sparse-backfill"
  end
  state.backfill_mode = "sparse"
  state.updated_at = as_string(payload.updated_at)
  state.updated_at_epoch = as_number(payload.updated_at_epoch)
  state.source = as_string(payload.source)
  state.last_sample_id = sample_id
  state.last_sample_anchor_epoch = as_number(payload.sample_anchor_epoch)
  state.last_sample_anchor_at = as_string(payload.sample_anchor_at)
  state.last_sample_window_seconds = as_number(payload.sample_window_seconds)
  state.sparse_total_samples = as_number(payload.sparse_total_samples)
  state.sparse_done_samples = redis.call("SCARD", done_key)
  state.sparse_headers_applied = as_number(state.sparse_headers_applied) + applied
  if latest_height > 0 then
    state.last_indexed_height = latest_height
    state.last_indexed_hash = as_string(headers[#headers].hash)
    state.last_indexed_timestamp_epoch = latest_timestamp_epoch
    state.last_indexed_at = latest_timestamp
  end
  redis.call("SET", state_key, json_encode(state))

  return json_encode({
    status = "ok",
    latest_block = latest_height,
    applied = applied,
    sparse_done_samples = state.sparse_done_samples,
    sparse_total_samples = state.sparse_total_samples,
  })
end)

redis.register_function("bdag_apply_trend_headers", function(keys, args)
  local payload = json_decode(args[1])
  local headers = payload.headers or {}
  if #headers <= 0 then
    return json_encode({status = "ignored", reason = "empty headers"})
  end

  local state_key = as_string(payload.state_key)
  if state_key == "" then
    state_key = "bdag:trend:v2:state"
  end
  local state = json_decode(redis.call("GET", state_key))
  local last_height = as_number(state.last_indexed_height)
  local last_hash = as_string(state.last_indexed_hash)
  local applied = 0
  local skipped = 0
  local latest_timestamp_epoch = as_number(state.last_indexed_timestamp_epoch)
  local latest_timestamp = as_string(state.last_indexed_at)
  local gap_at = 0
  local reorg = false
  local check_parent = as_bool(payload.check_parent, false)
  local updated_at = as_string(payload.updated_at)
  if updated_at == "" then
    updated_at = as_string(payload.received_at)
  end

  for _, header in ipairs(headers) do
    local height = as_number(header.height)
    local hash = as_string(header.hash)
    if height <= 0 or hash == "" then
      skipped = skipped + 1
    elseif height <= last_height then
      skipped = skipped + 1
    else
      if last_height > 0 and height ~= last_height + 1 then
        gap_at = height
        break
      end
      if last_height <= 0 and not as_bool(payload.allow_bootstrap, true) then
        return json_encode({status = "ignored", reason = "trend history is not initialized"})
      end
      local parent_hash = as_string(header.parent_hash)
      if check_parent and last_height > 0 and parent_hash ~= "" and last_hash ~= "" and string.lower(parent_hash) ~= string.lower(last_hash) then
        reorg = true
        redis.call("SET", "bdag:trend:v2:reorg_needed", json_encode({
          height = height,
          parent_hash = parent_hash,
          previous_hash = last_hash,
          detected_at = updated_at,
        }))
        break
      end
      if as_string(header.received_at) == "" then
        header.received_at = updated_at
      end
      if as_number(header.received_at_epoch) <= 0 then
        header.received_at_epoch = as_number(payload.updated_at_epoch)
      end
      for _, resolution in ipairs(trend_resolutions) do
        update_trend_bucket(header, resolution)
      end
      last_height = height
      last_hash = hash
      latest_timestamp_epoch = as_number(header.timestamp_epoch)
      latest_timestamp = as_string(header.timestamp)
      applied = applied + 1
    end
  end

  if applied > 0 then
    for _, resolution in ipairs(trend_resolutions) do
      trim_trend_resolution(resolution, latest_timestamp_epoch)
    end
    state.status = "ok"
    state.updated_at = updated_at
    state.updated_at_epoch = as_number(payload.updated_at_epoch)
    state.last_indexed_height = last_height
    state.last_indexed_hash = last_hash
    state.last_indexed_timestamp_epoch = latest_timestamp_epoch
    state.last_indexed_at = latest_timestamp
    state.source = as_string(payload.source)
    state.backfill_status = as_string(payload.backfill_status)
    state.resolutions = {10, 30, 60, 300, 900, 3600}
    redis.call("SET", state_key, json_encode(state))
  end

  if as_bool(payload.emit_event, false) and applied > 0 then
    dashboard_event("global", updated_at, {
      height = tostring(last_height),
      reason = "trend-updated",
      trend_applied = tostring(applied),
    })
  end

  if reorg then
    return json_encode({status = "reorg", latest_block = last_height, applied = applied, skipped = skipped, gap_at = gap_at})
  end
  if gap_at > 0 then
    return json_encode({status = "gap", latest_block = last_height, applied = applied, skipped = skipped, gap_at = gap_at})
  end
  if applied <= 0 then
    return json_encode({status = "duplicate", latest_block = last_height, applied = applied, skipped = skipped})
  end
  return json_encode({status = "ok", latest_block = last_height, applied = applied, skipped = skipped})
end)

redis.register_function("bdag_apply_pool_share", function(keys, args)
  local event = json_decode(args[1])
  local accepted = as_number(event.accepted)
  local rejected = as_number(event.rejected)
  local updated_at = as_string(event.updated_at)
  redis.call("XADD", "bdag:stream:pool:shares", "MAXLEN", "~", 2000, "*", "payload", json_encode(event))
  if accepted > 0 then
    redis.call("HINCRBY", "bdag:pool:share_counts", "accepted", accepted)
  end
  if rejected > 0 then
    redis.call("HINCRBY", "bdag:pool:share_counts", "rejected", rejected)
  end
  dashboard_event("pool", updated_at, {accepted = accepted, rejected = rejected})
  return json_encode({status = "ok"})
end)

redis.register_function("bdag_apply_block_submit", function(keys, args)
  local event = json_decode(args[1])
  local updated_at = as_string(event.updated_at)
  redis.call("XADD", "bdag:stream:pool:block_submits", "MAXLEN", "~", 2000, "*", "payload", json_encode(event))
  if event.accepted then
    redis.call("HINCRBY", "bdag:pool:block_submit_counts", "accepted", 1)
    redis.call("SET", "bdag:pool:last_accepted_block_submit", json_encode(event))
  else
    redis.call("HINCRBY", "bdag:pool:block_submit_counts", "rejected", 1)
  end
  dashboard_event("pool", updated_at, {block_submit = event.accepted and "accepted" or "rejected"})
  return json_encode({status = "ok"})
end)

redis.register_function("bdag_apply_asic_status", function(keys, args)
  local event = json_decode(args[1])
  local key = as_string(event.mac)
  if key == "" then
    key = as_string(event.ip)
  end
  if key ~= "" then
    redis.call("HSET", "bdag:miners:by_mac", key, json_encode(event))
  end
  redis.call("XADD", "bdag:stream:asic:status", "MAXLEN", "~", 1000, "*", "payload", json_encode(event))
  dashboard_event("asic", as_string(event.updated_at), {key = key})
  return json_encode({status = "ok"})
end)

redis.register_function("bdag_apply_host_metric", function(keys, args)
  local event = json_decode(args[1])
  local updated_at = as_string(event.updated_at)
  local snapshot = snapshot_envelope("ok", updated_at, 30, "redis-live", {}, event)
  redis.call("SET", "bdag:host:snapshot", json_encode(snapshot))
  redis.call("XADD", "bdag:stream:host:metrics", "MAXLEN", "~", 1000, "*", "payload", json_encode(event))
  dashboard_event("host", updated_at, {})
  return json_encode({status = "ok"})
end)

redis.register_function("bdag_mark_stale", function(keys, args)
  local now_epoch = as_number(args[1])
  local marked = {}
  for _, key in ipairs({"bdag:pool:snapshot", "bdag:chain:snapshot", "bdag:host:snapshot"}) do
    local raw = redis.call("GET", key)
    if raw then
      local payload = json_decode(raw)
      local updated = as_number(payload.updated_at_epoch)
      local stale_after = as_number(payload.stale_after_seconds)
      if stale_after <= 0 then
        stale_after = 30
      end
      if updated > 0 then
        payload.age_seconds = math.max(0, now_epoch - updated)
        payload.last_update_age_seconds = payload.age_seconds
        if payload.age_seconds > stale_after and payload.status ~= "stale" and payload.status ~= "error" then
          payload.status = "stale"
          payload.degraded_reasons = payload.degraded_reasons or {}
          table.insert(payload.degraded_reasons, "snapshot stale")
          table.insert(marked, key)
        end
        redis.call("SET", key, json_encode(payload))
      end
    end
  end
  if #marked > 0 then
    dashboard_event("stale", tostring(now_epoch), {marked = table.concat(marked, ",")})
  end
  return json_encode({status = "ok", marked = marked})
end)
`
