#!/usr/bin/env python3
"""Upload broad sample behavior data to the ms-data-receiver behavior API.

This script is intentionally kept close to the data receiver contract. If
`/api/v1/msdr/behavior/report` changes, update this script at the same time.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
from typing import Any


BEHAVIOR_PATH = "/api/v1/msdr/behavior/report"
MAX_BATCH_SIZE = 100


SUPPORTED_EVENT_TYPES = [
    "SHOW",
    "CLICK",
    "PAGE_VIEW",
    "SEARCH",
    "SEARCH_RESULT_SHOW",
    "SEARCH_RESULT_CLICK",
    "COMIC_DETAIL_VIEW",
    "CHAPTER_OPEN",
    "READ_START",
    "READ_PROGRESS",
    "READ_FINISH",
    "READ_EXIT",
    "READ_NEXT",
    "READ_PREV",
    "BOOKSHELF_ADD",
    "BOOKSHELF_REMOVE",
    "LIKE",
    "UNLIKE",
    "FAVORITE",
    "UNFAVORITE",
    "COMMENT",
    "COMMENT_REPLY",
    "COMMENT_DELETE",
    "COMMENT_REPORT",
    "SHARE",
    "FOLLOW",
    "UNFOLLOW",
    "DOWNLOAD_START",
    "DOWNLOAD_FINISH",
    "SUBSCRIBE",
    "UNSUBSCRIBE",
    "PURCHASE",
    "RECHARGE",
    "AD_SHOW",
    "AD_CLICK",
    "LOGIN",
    "REGISTER",
    "CUSTOM",
]


def env_first(names: tuple[str, ...], default: str) -> str:
    for name in names:
        value = os.getenv(name)
        if value:
            return value
    return default


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed <= 0:
        raise argparse.ArgumentTypeError("must be greater than 0")
    return parsed


def bounded_batch_size(value: str) -> int:
    parsed = positive_int(value)
    if parsed > MAX_BATCH_SIZE:
        raise argparse.ArgumentTypeError(f"must be <= {MAX_BATCH_SIZE}")
    return parsed


def event_slug(event_type: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", event_type.lower()).strip("-")


def behavior(
    event_id: str,
    event_type: str,
    *,
    action_at: int | None,
    platform: str | None = None,
    target_type: str | None = None,
    target_id: str | None = None,
    item_id: str | None = None,
    chapter_id: str | None = None,
    page_no: int | None = None,
    page_url: str | None = None,
    referrer: str | None = None,
    properties: dict[str, Any] | None = None,
) -> dict[str, Any]:
    data: dict[str, Any] = {
        "event_id": event_id,
        "event_type": event_type,
    }
    optional_values: dict[str, Any] = {
        "action_at": action_at,
        "platform": platform,
        "target_type": target_type,
        "target_id": target_id,
        "item_id": item_id,
        "chapter_id": chapter_id,
        "page_no": page_no,
        "page_url": page_url,
        "referrer": referrer,
        "properties": properties,
    }
    for key, value in optional_values.items():
        if value is not None:
            data[key] = value
    return data


def build_main_behaviors(item_prefix: str, run_id: str, action_base: int) -> list[dict[str, Any]]:
    item = lambda number: f"{item_prefix}{number:04d}"
    chapter = lambda number, chapter_number: f"{item(number)}-CH{chapter_number:03d}"
    event_seq = 0

    def make(event_type: str, **kwargs: Any) -> dict[str, Any]:
        nonlocal event_seq
        event_seq += 1
        kwargs.setdefault("action_at", action_base + event_seq * 30)
        return behavior(f"test-action-{run_id}-{event_seq:04d}-{event_slug(event_type)}", event_type, **kwargs)

    events = [
        make(
            "SHOW",
            platform="h5",
            target_type="COMIC",
            target_id=item(1),
            item_id=item(1),
            page_url="https://app.example.com/home",
            referrer="app://launch",
            properties={"slot": "home_feed", "rank": 1, "experiment": "rec_v2"},
        ),
        make(
            "CLICK",
            target_type="COMIC",
            target_id=item(1),
            item_id=item(1),
            page_url="https://app.example.com/comic/TST-C0001",
            referrer="https://app.example.com/home",
            properties={"slot": "home_feed", "rank": 1},
        ),
        make(
            "PAGE_VIEW",
            target_type="PAGE",
            target_id="home",
            page_url="https://app.example.com/home",
            properties={"page_name": "home", "tab": "recommend"},
        ),
        make(
            "SEARCH",
            target_type="SEARCH",
            target_id="search-space-adventure",
            page_url="https://app.example.com/search",
            properties={"query": "space adventure", "result_count": 32, "source": "search_box"},
        ),
        make(
            "SEARCH_RESULT_SHOW",
            target_type="COMIC",
            target_id=item(2),
            item_id=item(2),
            properties={"query": "space adventure", "rank": 2, "page": 1},
        ),
        make(
            "SEARCH_RESULT_CLICK",
            target_type="COMIC",
            target_id=item(2),
            item_id=item(2),
            properties={"query": "space adventure", "rank": 2, "page": 1},
        ),
        make("COMIC_DETAIL_VIEW", target_type="COMIC", target_id=item(3), item_id=item(3)),
        make("CHAPTER_OPEN", target_type="CHAPTER", target_id=chapter(3, 1), item_id=item(3), chapter_id=chapter(3, 1)),
        make("READ_START", target_type="CHAPTER", target_id=chapter(3, 1), item_id=item(3), chapter_id=chapter(3, 1), page_no=1),
        make(
            "READ_PROGRESS",
            target_type="CHAPTER",
            target_id=chapter(3, 1),
            item_id=item(3),
            chapter_id=chapter(3, 1),
            page_no=12,
            properties={"progress": 0.6, "duration_ms": 180000, "visible_pages": [10, 11, 12]},
        ),
        make(
            "READ_FINISH",
            target_type="CHAPTER",
            target_id=chapter(3, 1),
            item_id=item(3),
            chapter_id=chapter(3, 1),
            page_no=24,
            properties={"duration_ms": 360000, "auto_next": True},
        ),
        make(
            "READ_EXIT",
            target_type="CHAPTER",
            target_id=chapter(3, 2),
            item_id=item(3),
            chapter_id=chapter(3, 2),
            page_no=5,
            properties={"exit_reason": "back_button", "progress": 0.22},
        ),
        make("READ_NEXT", target_type="CHAPTER", target_id=chapter(3, 2), item_id=item(3), chapter_id=chapter(3, 2)),
        make("READ_PREV", target_type="CHAPTER", target_id=chapter(3, 1), item_id=item(3), chapter_id=chapter(3, 1)),
        make("BOOKSHELF_ADD", target_type="COMIC", target_id=item(4), item_id=item(4), properties={"source": "detail_page"}),
        make("BOOKSHELF_REMOVE", target_type="COMIC", target_id=item(4), item_id=item(4), properties={"source": "bookshelf"}),
        make("LIKE", target_type="COMIC", target_id=item(5), item_id=item(5), properties={"like_count_after": 101}),
        make("UNLIKE", target_type="COMIC", target_id=item(5), item_id=item(5), properties={"like_count_after": 100}),
        make("FAVORITE", target_type="CHAPTER", target_id=chapter(5, 3), item_id=item(5), chapter_id=chapter(5, 3)),
        make("UNFAVORITE", target_type="CHAPTER", target_id=chapter(5, 3), item_id=item(5), chapter_id=chapter(5, 3)),
        make("COMMENT", target_type="COMMENT", target_id=f"COMMENT-{run_id}-001", item_id=item(6), properties={"length": 42, "contains_image": False}),
        make("COMMENT_REPLY", target_type="COMMENT", target_id=f"COMMENT-{run_id}-001", item_id=item(6), properties={"reply_depth": 1}),
        make("COMMENT_DELETE", target_type="COMMENT", target_id=f"COMMENT-{run_id}-002", item_id=item(6), properties={"delete_reason": "self"}),
        make("COMMENT_REPORT", target_type="COMMENT", target_id=f"COMMENT-{run_id}-003", item_id=item(6), properties={"reason": "spam"}),
        make("SHARE", target_type="COMIC", target_id=item(7), item_id=item(7), properties={"share_channel": "wechat", "with_quote": True}),
        make("FOLLOW", target_type="AUTHOR", target_id="AUTHOR-1001", properties={"source": "author_page"}),
        make("UNFOLLOW", target_type="AUTHOR", target_id="AUTHOR-1001", properties={"source": "following_list"}),
        make("DOWNLOAD_START", target_type="CHAPTER", target_id=chapter(8, 1), item_id=item(8), chapter_id=chapter(8, 1)),
        make("DOWNLOAD_FINISH", target_type="CHAPTER", target_id=chapter(8, 1), item_id=item(8), chapter_id=chapter(8, 1), properties={"size_mb": 12.4}),
        make("SUBSCRIBE", target_type="COMIC", target_id=item(9), item_id=item(9), properties={"auto_purchase": True}),
        make("UNSUBSCRIBE", target_type="COMIC", target_id=item(9), item_id=item(9), properties={"auto_purchase": False}),
        make(
            "PURCHASE",
            target_type="ORDER",
            target_id=f"ORDER-{run_id}-001",
            item_id=item(10),
            chapter_id=chapter(10, 5),
            properties={"amount": 12.0, "currency": "CNY", "pay_method": "balance", "paid": True},
        ),
        make(
            "RECHARGE",
            target_type="ORDER",
            target_id=f"ORDER-{run_id}-002",
            properties={"amount": 68.0, "currency": "CNY", "package": "coin_680"},
        ),
        make("AD_SHOW", target_type="AD", target_id="AD-BANNER-001", item_id=item(11), properties={"slot": "reader_bottom", "creative": "banner_a"}),
        make("AD_CLICK", target_type="AD", target_id="AD-BANNER-001", item_id=item(11), properties={"slot": "reader_bottom", "creative": "banner_a"}),
        make("LOGIN", target_type="USER", target_id="U10001", properties={"method": "password", "success": True}),
        make("REGISTER", target_type="USER", target_id=f"U-NEW-{run_id}", properties={"method": "email", "campaign": "spring"}),
        make(
            "CUSTOM",
            target_type="PAGE",
            target_id="experiment-panel",
            item_id=item(12),
            properties={
                "name": "experiment_exposure",
                "bucket": "B",
                "attrs": {"module": "recommend", "rule": "nearline"},
            },
        ),
    ]

    if [event["event_type"] for event in events] != SUPPORTED_EVENT_TYPES:
        raise RuntimeError("main behavior list no longer matches SUPPORTED_EVENT_TYPES")
    return events


def extra_identity_payloads(item_prefix: str, run_id: str, action_base: int) -> list[tuple[str, dict[str, Any]]]:
    item = lambda number: f"{item_prefix}{number:04d}"
    return [
        (
            "session-only",
            {
                "session_id": f"S-ONLY-{run_id}",
                "platform": "h5",
                "device_type": "H5",
                "network_type": "5G",
                "behaviors": [
                    behavior(
                        f"test-action-{run_id}-session-only-show",
                        "SHOW",
                        action_at=action_base + 5000,
                        target_type="COMIC",
                        target_id=item(13),
                        item_id=item(13),
                        properties={"slot": "anonymous_home", "rank": 3},
                    )
                ],
            },
        ),
        (
            "anonymous-only",
            {
                "anonymous_id": f"A-ONLY-{run_id}",
                "platform": "web",
                "device_type": "WEB",
                "network_type": "ETHERNET",
                "screen_width": 1440,
                "screen_height": 900,
                "behaviors": [
                    behavior(
                        f"test-action-{run_id}-anonymous-page-view",
                        "PAGE_VIEW",
                        action_at=action_base + 5030,
                        target_type="PAGE",
                        target_id="landing",
                        page_url="https://app.example.com/landing",
                        properties={"page_name": "landing", "entry": "direct"},
                    )
                ],
            },
        ),
        (
            "server-default-action-at",
            {
                "user_id": f"U-DEFAULT-TIME-{run_id}",
                "device_type": "ANDROID",
                "network_type": "UNKNOWN",
                "behaviors": [
                    behavior(
                        f"test-action-{run_id}-server-default-action-at",
                        "CUSTOM",
                        action_at=None,
                        target_type="PAGE",
                        target_id="default-action-at-check",
                        item_id=item(14),
                        properties={"coverage": "missing_action_at"},
                    )
                ],
            },
        ),
    ]


def chunked(items: list[dict[str, Any]], size: int) -> list[list[dict[str, Any]]]:
    return [items[index : index + size] for index in range(0, len(items), size)]


def behavior_payload(behaviors: list[dict[str, Any]]) -> dict[str, Any]:
    return {
        "user_id": "U10001",
        "anonymous_id": "A90001",
        "session_id": "S202605190001",
        "platform": "ios",
        "device_id": "D10001",
        "device_type": "IOS",
        "device_brand": "Apple",
        "device_model": "iPhone 15",
        "device_version": "iOS 18.1",
        "app_version": "3.8.1",
        "app_channel": "AppStore",
        "os": "iOS",
        "network_type": "WIFI",
        "carrier": "China Mobile",
        "ip": "203.0.113.10",
        "country": "CN",
        "province": "Shanghai",
        "city": "Shanghai",
        "language": "zh-CN",
        "timezone": "Asia/Shanghai",
        "screen_width": 1179,
        "screen_height": 2556,
        "user_agent": "ms-sar-dashboard-test-action-upload/1.0",
        "behaviors": behaviors,
    }


def post_json(url: str, payload: dict[str, Any], headers: dict[str, str], timeout: float) -> dict[str, Any]:
    body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    request = urllib.request.Request(url, data=body, method="POST", headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            response_body = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        response_body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {exc.code} from {url}: {response_body}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"failed to connect to {url}: {exc}") from exc

    try:
        decoded = json.loads(response_body)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"invalid JSON response from {url}: {response_body}") from exc
    if decoded.get("status") != 200:
        raise RuntimeError(f"API rejected request: {json.dumps(decoded, ensure_ascii=False)}")
    return decoded


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Upload sample behavior data to ms-data-receiver.")
    parser.add_argument(
        "--base-url",
        default=env_first(("DATA_RECEIVER_BASE_URL", "BASE_URL"), "http://127.0.0.1:8080"),
        help="ms-data-receiver base URL. Env: DATA_RECEIVER_BASE_URL or BASE_URL.",
    )
    parser.add_argument(
        "--appid",
        default=env_first(("DWZ_APPID", "APPID"), "100001"),
        help="x-dwzauth-appid header. Env: DWZ_APPID or APPID.",
    )
    parser.add_argument(
        "--secret",
        default=env_first(("DWZ_APP_SECRET", "APP_SECRET"), "secret-1"),
        help="x-dwzauth-secret header. Env: DWZ_APP_SECRET or APP_SECRET.",
    )
    parser.add_argument(
        "--item-prefix",
        default=os.getenv("TEST_ITEM_PREFIX", "TST-C"),
        help="prefix used to build item_id values matching test-items-upload.py.",
    )
    parser.add_argument(
        "--run-id",
        default=os.getenv("TEST_ACTION_RUN_ID") or str(int(time.time())),
        help="unique suffix for event_id values. Reusing it exercises dedupe.",
    )
    parser.add_argument(
        "--batch-size",
        type=bounded_batch_size,
        default=MAX_BATCH_SIZE,
        help=f"behaviors per request, max {MAX_BATCH_SIZE}.",
    )
    parser.add_argument("--timeout", type=float, default=10.0, help="HTTP timeout in seconds.")
    parser.add_argument("--dry-run", action="store_true", help="print payloads without posting them.")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    base_url = args.base_url.rstrip("/")
    url = f"{base_url}{BEHAVIOR_PATH}"
    action_base = int(time.time()) - 1800

    main_batches = [
        (f"main-{index}", behavior_payload(batch))
        for index, batch in enumerate(chunked(build_main_behaviors(args.item_prefix, args.run_id, action_base), args.batch_size), start=1)
    ]
    payloads = main_batches + extra_identity_payloads(args.item_prefix, args.run_id, action_base)

    headers = {
        "Content-Type": "application/json",
        "User-Agent": "ms-sar-dashboard-test-action-upload/1.0",
        "x-dwzauth-appid": str(args.appid),
        "x-dwzauth-secret": str(args.secret),
    }

    if args.dry_run:
        print(json.dumps({"url": url, "headers": {**headers, "x-dwzauth-secret": "***"}, "payloads": payloads}, ensure_ascii=False, indent=2))
        return 0

    total_accepted = 0
    total_published = 0
    total_duplicated = 0
    for index, (name, payload) in enumerate(payloads, start=1):
        batch_headers = {
            **headers,
            "x-request-id": f"test-action-upload-{args.run_id}-{index}",
        }
        response = post_json(url, payload, batch_headers, args.timeout)
        data = response.get("data") or {}
        accepted = int(data.get("accepted", 0))
        published = int(data.get("published", 0))
        duplicated = int(data.get("duplicated", 0))
        total_accepted += accepted
        total_published += published
        total_duplicated += duplicated
        print(
            f"[actions] payload={name} accepted={accepted} published={published} "
            f"duplicated={duplicated} request_id={response.get('request_id')}"
        )

    print(
        f"[actions] done run_id={args.run_id} accepted={total_accepted} "
        f"published={total_published} duplicated={total_duplicated}"
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except RuntimeError as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)
