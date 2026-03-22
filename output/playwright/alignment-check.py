from playwright.sync_api import sync_playwright
import json

def get_box(page, selector):
    el = page.locator(selector).first
    if el.count() == 0:
        return None
    return el.bounding_box()

def get_all_boxes(page, selector):
    els = page.locator(selector)
    count = els.count()
    results = []
    for i in range(count):
        box = els.nth(i).bounding_box()
        text = els.nth(i).inner_text()[:60].replace('\n', ' ')
        if box:
            results.append({"index": i, "text": text, "x": round(box["x"], 1), "y": round(box["y"], 1), "w": round(box["width"], 1), "h": round(box["height"], 1)})
    return results

def check_row_alignment(label, items):
    """Check if items in the same grid row have aligned tops"""
    if len(items) < 2:
        return
    # Group by approximate Y position (within 5px = same row)
    rows = {}
    for item in items:
        row_key = round(item["y"] / 20) * 20
        if row_key not in rows:
            rows[row_key] = []
        rows[row_key].append(item)

    for row_y, row_items in rows.items():
        if len(row_items) >= 2:
            ys = [i["y"] for i in row_items]
            max_diff = max(ys) - min(ys)
            if max_diff > 1:
                print(f"  MISALIGNED [{label}] row ~{row_y}: y-offsets differ by {max_diff:.1f}px")
                for i in row_items:
                    print(f"    [{i['index']}] y={i['y']} h={i['h']} '{i['text'][:40]}'")
            else:
                print(f"  OK [{label}] row ~{row_y}: {len(row_items)} items aligned (diff={max_diff:.1f}px)")

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)

    # ===== OVERVIEW PAGE =====
    print("=" * 60)
    print("OVERVIEW PAGE ALIGNMENT CHECK")
    print("=" * 60)

    page = browser.new_page(viewport={"width": 1440, "height": 5000})
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(3000)

    # Check card positions in each layout row
    print("\n--- Layout Cards ---")
    cards = get_all_boxes(page, ".layout .card")
    check_row_alignment("layout cards", cards)

    # Check titles within adjacent cards
    print("\n--- Section Titles ---")
    titles = get_all_boxes(page, ".section-head .title")
    check_row_alignment("section titles", titles)

    # Check captions within adjacent cards
    print("\n--- Section Captions ---")
    captions = get_all_boxes(page, ".section-head .caption")
    check_row_alignment("section captions", captions)

    # Check surface-strip alignment
    print("\n--- Surface Strips ---")
    strips = get_all_boxes(page, ".surface-strip")
    check_row_alignment("surface strips", strips)

    # Check metrics grids
    print("\n--- Metrics Grids ---")
    metrics = get_all_boxes(page, ".metrics")
    check_row_alignment("metrics grids", metrics)

    # Check individual metric cards in performance
    print("\n--- Performance Metric Cards ---")
    perf_metrics = get_all_boxes(page, "#metrics .metric")
    check_row_alignment("perf metrics", perf_metrics)

    # Check hero-copy vs hero-side
    print("\n--- Hero Columns ---")
    hero_copy = get_box(page, ".hero-copy")
    hero_side = get_box(page, ".hero-side")
    if hero_copy and hero_side:
        diff = abs(hero_copy["y"] - hero_side["y"])
        print(f"  hero-copy y={hero_copy['y']:.1f} hero-side y={hero_side['y']:.1f} diff={diff:.1f}px")

    # Check hero priority grid cards
    print("\n--- Hero Priority Cards ---")
    hero_cards = get_all_boxes(page, ".hero-priority-grid .surface-card")
    check_row_alignment("hero priority", hero_cards)

    # Check chart cards
    print("\n--- Chart Cards ---")
    chart_cards = get_all_boxes(page, "#chartLayout .card")
    check_row_alignment("chart cards", chart_cards)

    # Check topbar brand vs nav
    print("\n--- Topbar ---")
    brand = get_box(page, ".brand")
    topnav = get_box(page, ".topnav")
    if brand and topnav:
        brand_center = brand["y"] + brand["height"] / 2
        nav_center = topnav["y"] + topnav["height"] / 2
        print(f"  brand center={brand_center:.1f} nav center={nav_center:.1f} diff={abs(brand_center - nav_center):.1f}px")

    # Check mini-chips alignment in section-meta-strip
    print("\n--- Section Meta Strips ---")
    meta_strips = get_all_boxes(page, ".section-meta-strip")
    for ms in meta_strips:
        print(f"  meta-strip [{ms['index']}] y={ms['y']} h={ms['h']} '{ms['text'][:30]}'")

    page.close()

    # ===== SETTINGS PAGE =====
    print("\n" + "=" * 60)
    print("SETTINGS PAGE ALIGNMENT CHECK")
    print("=" * 60)

    page2 = browser.new_page(viewport={"width": 1440, "height": 5000})
    page2.goto("http://127.0.0.1:18080/admin/settings?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page2.wait_for_load_state("networkidle")
    page2.wait_for_timeout(3000)

    # Check 3-column shell alignment
    print("\n--- Settings Shell Columns ---")
    nav_panel = get_box(page2, ".settings-nav")
    main_panel = get_box(page2, ".settings-main")
    rail_panel = get_box(page2, ".settings-rail")
    if nav_panel and main_panel and rail_panel:
        print(f"  nav:  x={nav_panel['x']:.1f} y={nav_panel['y']:.1f} w={nav_panel['width']:.1f}")
        print(f"  main: x={main_panel['x']:.1f} y={main_panel['y']:.1f} w={main_panel['width']:.1f}")
        print(f"  rail: x={rail_panel['x']:.1f} y={rail_panel['y']:.1f} w={rail_panel['width']:.1f}")
        top_diff = max(nav_panel['y'], main_panel['y'], rail_panel['y']) - min(nav_panel['y'], main_panel['y'], rail_panel['y'])
        print(f"  top alignment diff: {top_diff:.1f}px")

    # Check config sections alignment
    print("\n--- Config Sections ---")
    sections = get_all_boxes(page2, ".config-section")
    for s in sections:
        print(f"  section [{s['index']}] x={s['x']} y={s['y']} w={s['w']} '{s['text'][:30]}'")

    # Check config-card-head alignment
    print("\n--- Config Card Heads ---")
    heads = get_all_boxes(page2, ".config-card-head")
    for h in heads:
        print(f"  head [{h['index']}] x={h['x']} y={h['y']} w={h['w']} '{h['text'][:40]}'")

    # Check jumpbar links alignment
    print("\n--- Jumpbar Links ---")
    jumpbar = get_all_boxes(page2, ".settings-jumpbar a")
    for j in jumpbar:
        print(f"  link [{j['index']}] x={j['x']} y={j['y']} w={j['w']} h={j['h']} '{j['text'][:30]}'")

    # Check config-grid fields alignment
    print("\n--- Config Grid Fields (first section) ---")
    fields = get_all_boxes(page2, "#cfg-health .config-field")
    check_row_alignment("health fields", fields)

    # Check policy grid cards
    print("\n--- Policy Grid ---")
    policy_cards = get_all_boxes(page2, ".policy-grid .policy-card")
    check_row_alignment("policy cards", policy_cards)

    # Check mode preset grid
    print("\n--- Mode Preset Grid ---")
    presets = get_all_boxes(page2, ".mode-preset-grid .mode-preset")
    check_row_alignment("mode presets", presets)

    # Check provider summary strips
    print("\n--- Provider Summary Strips ---")
    prov_strips = get_all_boxes(page2, ".provider-summary-strip")
    for ps in prov_strips:
        print(f"  strip [{ps['index']}] x={ps['x']} y={ps['y']} w={ps['w']} '{ps['text'][:40]}'")

    # Check rail action buttons
    print("\n--- Rail Action Buttons ---")
    rail_btns = get_all_boxes(page2, ".settings-rail-actions .btn")
    check_row_alignment("rail buttons", rail_btns)

    page2.close()
    browser.close()
    print("\nDone.")
