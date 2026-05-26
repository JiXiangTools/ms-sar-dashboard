#!/usr/bin/env python3
"""Upload broad sample item data to the ms-data-receiver product API.

This script is intentionally kept close to the data receiver contract. If
`/api/v1/msdr/product/report` changes, update this script at the same time.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from typing import Any


PRODUCT_PATH = "/api/v1/msdr/product/report"
MAX_BATCH_SIZE = 100


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


def product(
    item_id: str,
    title: str,
    *,
    author: str | None = None,
    summary: str | None = None,
    content: str | None = None,
    categories: list[str] | None = None,
    tags: list[str] | None = None,
    disabled: bool | None = None,
    price: int | None = None,
    publish_at: int | None = None,
    other: dict[str, Any] | None = None,
) -> dict[str, Any]:
    data: dict[str, Any] = {
        "item_id": item_id,
        "title": title,
    }
    optional_values: dict[str, Any] = {
        "author": author,
        "summary": summary,
        "content": content,
        "categories": categories,
        "tags": tags,
        "disabled": disabled,
        "price": price,
        "publish_at": publish_at,
        "other": other,
    }
    for key, value in optional_values.items():
        if value is not None:
            data[key] = value
    return data


def build_products(item_prefix: str, publish_base: int) -> list[dict[str, Any]]:
    item = lambda number: f"{item_prefix}{number:04d}"
    return [
        product(
            item(1),
            "Star Harbor",
            author="North Studio",
            summary="A long-running space adventure series.",
            content=(
                "A young pilot follows a broken star map through abandoned orbital "
                "cities, lost archives, and a civil war at the edge of known space."
            ),
            categories=["comics", "sci-fi", "adventure"],
            tags=["space", "teen", "weekly", "featured"],
            disabled=False,
            price=0,
            publish_at=publish_base,
            other={
                "cover_url": "https://cdn.example.com/comics/star-harbor.jpg",
                "chapter_count": 48,
                "rating": 9.2,
                "finished": False,
                "source": "test-items-upload",
            },
        ),
        product(
            item(2),
            "Rain City Letters",
            author="Ink Line",
            summary="A gentle romance told through letters and cafe encounters.",
            content="Two strangers keep finding each other's notes during a rainy season.",
            categories=["comics", "romance", "slice-of-life"],
            tags=["slow-burn", "urban", "healing"],
            disabled=False,
            price=5,
            publish_at=publish_base - 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/rain-city-letters.jpg",
                "chapter_count": 24,
                "rating": 8.7,
                "finished": True,
            },
        ),
        product(
            item(3),
            "Iron Orchard",
            author="Red Gate",
            summary="Post-apocalyptic farming, giant machines, and found family.",
            content="A settlement rebuilds an orchard while defending itself from machines.",
            categories=["comics", "fantasy", "action"],
            tags=["mecha", "survival", "team"],
            disabled=False,
            price=8,
            publish_at=publish_base - 2 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/iron-orchard.jpg",
                "chapter_count": 60,
                "rating": 8.9,
                "finished": False,
            },
        ),
        product(
            item(4),
            "Midnight Archive",
            author="Grey Room",
            summary="Mystery cases hidden inside an endless library.",
            content="A librarian traces missing memories through locked rooms and cold cases.",
            categories=["comics", "mystery"],
            tags=["detective", "supernatural", "suspense"],
            disabled=False,
            price=12,
            publish_at=publish_base - 3 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/midnight-archive.jpg",
                "chapter_count": 36,
                "rating": 9.0,
                "finished": False,
            },
        ),
        product(
            item(5),
            "Kitchen Knights",
            author="Table Seven",
            summary="Competitive cooking with a fantasy tournament frame.",
            content="A novice chef wins a place in a tournament judged by wandering kings.",
            categories=["comics", "comedy", "food"],
            tags=["cooking", "competition", "light"],
            disabled=False,
            price=3,
            publish_at=publish_base - 4 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/kitchen-knights.jpg",
                "chapter_count": 18,
                "rating": 8.3,
                "finished": False,
            },
        ),
        product(
            item(6),
            "Neon Drifter",
            author="Blue Circuit",
            summary="Cyberpunk courier missions in a split-level megacity.",
            content="A courier learns that one package can rewrite a city's operating system.",
            categories=["comics", "cyberpunk", "action"],
            tags=["future", "chase", "tech"],
            disabled=False,
            price=10,
            publish_at=publish_base - 5 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/neon-drifter.jpg",
                "chapter_count": 42,
                "rating": 8.8,
                "finished": False,
            },
        ),
        product(
            item(7),
            "Cloud School",
            author="Morning Bell",
            summary="Campus comedy set in a flying academy.",
            content="First-year students learn weather magic and accidentally reroute seasons.",
            categories=["comics", "school", "fantasy"],
            tags=["campus", "magic", "friendship"],
            disabled=False,
            price=0,
            publish_at=publish_base - 6 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/cloud-school.jpg",
                "chapter_count": 30,
                "rating": 8.5,
                "finished": False,
            },
        ),
        product(
            item(8),
            "Last Train North",
            author="Signal House",
            summary="A thriller about passengers trapped on a night train.",
            content="Every station repeats, but the passengers remember different timelines.",
            categories=["comics", "thriller"],
            tags=["loop", "train", "suspense"],
            disabled=False,
            price=9,
            publish_at=publish_base - 7 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/last-train-north.jpg",
                "chapter_count": 22,
                "rating": 8.6,
                "finished": True,
            },
        ),
        product(
            item(9),
            "Pixel Guild",
            author="Quest Lab",
            summary="Game-world comedy with guild raids and bug exploits.",
            content="A QA tester wakes up inside the game she was hired to break.",
            categories=["comics", "game", "comedy"],
            tags=["isekai", "guild", "level-up"],
            disabled=False,
            price=6,
            publish_at=publish_base - 8 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/pixel-guild.jpg",
                "chapter_count": 55,
                "rating": 8.4,
                "finished": False,
            },
        ),
        product(
            item(10),
            "Quiet Volcano",
            author="Stone Paper",
            summary="Family drama on an island with an awakening volcano.",
            content="A geologist returns home and uncovers old promises before an eruption.",
            categories=["comics", "drama"],
            tags=["family", "island", "slow"],
            disabled=False,
            price=7,
            publish_at=publish_base - 9 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/quiet-volcano.jpg",
                "chapter_count": 16,
                "rating": 8.1,
                "finished": False,
            },
        ),
        product(
            item(11),
            "Mirror Botanist",
            author="Green Glass",
            summary="A botanist grows impossible plants from reflected worlds.",
            content="Every plant opens a different path, but each path asks for a memory.",
            categories=["comics", "fantasy", "mystery"],
            tags=["plants", "portal", "poetic"],
            disabled=False,
            price=4,
            publish_at=publish_base - 10 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/mirror-botanist.jpg",
                "chapter_count": 28,
                "rating": 8.9,
                "finished": False,
            },
        ),
        product(
            item(12),
            "Offline Planet",
            author="Patch Notes",
            summary="A small colony learns to live without network access.",
            content="Engineers, farmers, and musicians rebuild society when signals vanish.",
            categories=["comics", "sci-fi"],
            tags=["colony", "engineering", "hopeful"],
            disabled=False,
            price=2,
            publish_at=publish_base - 11 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/offline-planet.jpg",
                "chapter_count": 12,
                "rating": 8.0,
                "finished": False,
            },
        ),
        product(
            item(13),
            "Retired Hero Service",
            author="Town Square",
            summary="A retired hero opens a neighborhood repair shop.",
            content="Old rivals bring broken appliances, curses, and unresolved feelings.",
            categories=["comics", "comedy", "fantasy"],
            tags=["hero", "daily-life", "warm"],
            disabled=False,
            price=0,
            publish_at=publish_base - 12 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/retired-hero-service.jpg",
                "chapter_count": 40,
                "rating": 8.6,
                "finished": False,
            },
        ),
        product(
            item(14),
            "Deep Sea Courier",
            author="Abyss Mail",
            summary="Ocean delivery routes between undersea cities.",
            content="A courier crosses whale roads, coral archives, and pressure storms.",
            categories=["comics", "adventure"],
            tags=["ocean", "journey", "worldbuilding"],
            disabled=False,
            price=11,
            publish_at=publish_base - 13 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/deep-sea-courier.jpg",
                "chapter_count": 34,
                "rating": 8.8,
                "finished": False,
            },
        ),
        product(
            item(15),
            "The Draft Shelf",
            author="Archive Bot",
            summary="Disabled sample item for visibility and downstream filtering checks.",
            content="This item is intentionally marked disabled in the test upload.",
            categories=["comics", "test"],
            tags=["disabled", "draft"],
            disabled=True,
            price=0,
            publish_at=publish_base - 14 * 86400,
            other={
                "cover_url": "https://cdn.example.com/comics/the-draft-shelf.jpg",
                "chapter_count": 1,
                "rating": 0,
                "finished": False,
            },
        ),
        product(item(16), "Minimal Required Item"),
    ]


def chunked(items: list[dict[str, Any]], size: int) -> list[list[dict[str, Any]]]:
    return [items[index : index + size] for index in range(0, len(items), size)]


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
    parser = argparse.ArgumentParser(description="Upload sample item data to ms-data-receiver.")
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
        help="prefix used to build item_id values.",
    )
    parser.add_argument(
        "--batch-size",
        type=bounded_batch_size,
        default=MAX_BATCH_SIZE,
        help=f"items per request, max {MAX_BATCH_SIZE}.",
    )
    parser.add_argument("--timeout", type=float, default=10.0, help="HTTP timeout in seconds.")
    parser.add_argument("--dry-run", action="store_true", help="print payloads without posting them.")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    base_url = args.base_url.rstrip("/")
    url = f"{base_url}{PRODUCT_PATH}"
    publish_base = int(time.time()) - 3600
    products = build_products(args.item_prefix, publish_base)

    headers = {
        "Content-Type": "application/json",
        "User-Agent": "ms-sar-dashboard-test-items-upload/1.0",
        "x-dwzauth-appid": str(args.appid),
        "x-dwzauth-secret": str(args.secret),
    }

    batches = chunked(products, args.batch_size)
    if args.dry_run:
        print(json.dumps({"url": url, "headers": {**headers, "x-dwzauth-secret": "***"}, "batches": batches}, ensure_ascii=False, indent=2))
        return 0

    total_accepted = 0
    total_published = 0
    for index, batch in enumerate(batches, start=1):
        batch_headers = {
            **headers,
            "x-request-id": f"test-items-upload-{int(time.time())}-{index}",
        }
        response = post_json(url, {"products": batch}, batch_headers, args.timeout)
        data = response.get("data") or {}
        accepted = int(data.get("accepted", 0))
        published = int(data.get("published", 0))
        total_accepted += accepted
        total_published += published
        print(
            f"[items] batch={index}/{len(batches)} accepted={accepted} "
            f"published={published} request_id={response.get('request_id')}"
        )

    print(f"[items] done total_items={len(products)} accepted={total_accepted} published={total_published}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except RuntimeError as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)
