#!/usr/bin/env python3
"""Report current behavior-contract samples; --dry-run never sends data."""
from __future__ import annotations
import argparse
import json
import os
import secrets
import time
import urllib.error
import urllib.request

SUPPORTED_EVENT_TYPES = ['SHOW', 'CLICK', 'PAGE_VIEW', 'SEARCH', 'SEARCH_RESULT_SHOW', 'SEARCH_RESULT_CLICK', 'ITEM_DETAIL_VIEW', 'ITEM_OPEN', 'CONTENT_START', 'CONTENT_PROGRESS', 'CONTENT_FINISH', 'CONTENT_EXIT', 'CONTENT_NEXT', 'CONTENT_PREV', 'COLLECTION_ADD', 'COLLECTION_REMOVE', 'LIKE', 'UNLIKE', 'FAVORITE', 'UNFAVORITE', 'COMMENT', 'COMMENT_REPLY', 'COMMENT_DELETE', 'COMMENT_REPORT', 'SHARE', 'FOLLOW', 'UNFOLLOW', 'DOWNLOAD_START', 'DOWNLOAD_FINISH', 'SUBSCRIBE', 'UNSUBSCRIBE', 'PURCHASE', 'TIP', 'RECHARGE', 'AD_SHOW', 'AD_CLICK', 'LOGIN', 'REGISTER']
MAX_BATCH_SIZE = 100
BEHAVIOR_PATH = "/api/v1/msdr/behavior/report"


def build_main_behaviors(item_prefix, run_id, action_base):
    events = []
    for index, kind in enumerate(SUPPORTED_EVENT_TYPES):
        item = f"{item_prefix}{index % 12 + 1:04d}"
        target, target_id = "ITEM", item
        properties = {}
        if kind == "PAGE_VIEW": target, target_id = "VIEW", "home"
        if kind == "SEARCH": target, target_id = "SEARCH", "sample-query"
        if kind in {"FOLLOW", "UNFOLLOW"}: target, target_id = "AUTHOR", "author1"
        if kind in {"LOGIN", "REGISTER"}: target, target_id = "USER", "U10001"
        if kind in {"PURCHASE", "RECHARGE"}:
            target, target_id = "ORDER", f"order-{run_id}-{index}"
            properties = {"transaction_id": target_id, "amount_minor": 1200, "currency": "CNY"}
        if kind == "TIP":
            properties = {"transaction_id": f"tip-{run_id}", "amount_minor": 100, "currency": "CNY"}
        if kind in {"AD_SHOW", "AD_CLICK"}:
            item = "ad_sample"
            target, target_id = "AD", item
            properties = {"placement": "READER"}
        event = {"event_id": f"{run_id}-{index}", "event_type": kind,
                 "action_at": action_base + index, "platform": "ios",
                 "target_type": target, "target_id": target_id, "scene": "home"}
        if target in {"ITEM", "AD", "ORDER"}: event["item_id"] = item
        if kind.startswith("CONTENT_"): event["duration_ms"] = 3000
        if properties: event["properties"] = properties
        events.append(event)
    return events


def behavior_payload(events):
    return {"user_id": "U10001", "session_id": "session1", "anonymous_id": "anon1",
            "platform": "ios", "device_id": "D10001", "device_type": "IOS",
            "device_brand": "Apple", "device_model": "iPhone", "device_version": "18.1",
            "app_version": "1.0.0", "app_channel": "appstore", "os": "iOS",
            "network_type": "WIFI", "language": "zh-CN", "timezone": "Asia/Shanghai",
            "screen_width": 390, "screen_height": 844, "behaviors": events}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default=os.getenv("DATA_RECEIVER_BASE_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--appid", default=os.getenv("DWZ_APPID", "100001"))
    parser.add_argument("--secret", default=os.getenv("DWZ_APP_SECRET", ""))
    parser.add_argument("--run-id", default=secrets.token_hex(4))
    parser.add_argument("--item-prefix", default=os.getenv("TEST_ITEM_PREFIX", "TST-C"))
    parser.add_argument("--batch-size", type=int, default=100)
    parser.add_argument("--timeout", type=float, default=15)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    if not 1 <= args.batch_size <= 100: parser.error("batch-size must be 1..100")
    if not args.dry_run and not args.secret: parser.error("DWZ_APP_SECRET or --secret is required")
    events = build_main_behaviors(args.item_prefix, args.run_id, int(time.time()) - 60)
    payloads = [behavior_payload(events[i:i+args.batch_size]) for i in range(0,len(events),args.batch_size)]
    for identity in ("session_id", "anonymous_id"):
        payloads.append({identity: f"{identity}-{args.run_id}", "behaviors": [{"event_type":"PAGE_VIEW", "event_id":f"{args.run_id}-{identity}", "action_at":int(time.time()), "target_type":"VIEW", "target_id":"home"}]})
    if args.dry_run:
        print(json.dumps({"url": args.base_url + BEHAVIOR_PATH, "payloads": payloads}, ensure_ascii=False, indent=2))
        return 0
    for payload in payloads:
        request = urllib.request.Request(args.base_url.rstrip("/") + BEHAVIOR_PATH, data=json.dumps(payload).encode(), headers={"Content-Type":"application/json", "x-dwzauth-appid":args.appid, "x-dwzauth-secret":args.secret, "x-request-id":secrets.token_hex(8)})
        with urllib.request.urlopen(request, timeout=args.timeout) as response:
            result = json.load(response)
        data = result.get("data", {})
        if result.get("status") != 200 or data.get("failed",0):
            raise RuntimeError("behavior sample rejected: " + json.dumps(result,ensure_ascii=False))
        print(json.dumps(data,ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
