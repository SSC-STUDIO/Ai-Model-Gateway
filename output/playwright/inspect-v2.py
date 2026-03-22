from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 900})
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(4000)

    # Check card heights in each layout row
    print("=== CARD HEIGHT ALIGNMENT ===")
    layouts = page.locator(".layout")
    for li in range(layouts.count()):
        layout = layouts.nth(li)
        layout_class = layout.get_attribute("class") or ""
        cards = layout.locator("> .card")
        print(f"\nLayout [{li}] '{layout_class}':")
        card_data = []
        for ci in range(cards.count()):
            card = cards.nth(ci)
            box = card.bounding_box()
            card_id = card.get_attribute("id") or ""
            card_cls = card.get_attribute("class") or ""
            if box:
                card_data.append({"id": card_id, "cls": card_cls[:40], "y": box["y"], "h": box["height"], "w": box["width"]})
                print(f"  [{ci}] id={card_id:20s} y={box['y']:.0f} h={box['height']:.0f} w={box['width']:.0f}")

        # Check pairs in same row
        for i in range(len(card_data)):
            for j in range(i+1, len(card_data)):
                a, b = card_data[i], card_data[j]
                if abs(a["y"] - b["y"]) < 20:  # same row
                    h_diff = abs(a["h"] - b["h"])
                    if h_diff > 2:
                        print(f"  ⚠ HEIGHT MISMATCH: '{a['id']}' h={a['h']:.0f} vs '{b['id']}' h={b['h']:.0f} (diff={h_diff:.0f}px)")
                    else:
                        print(f"  ✓ ALIGNED: '{a['id']}' and '{b['id']}' (diff={h_diff:.1f}px)")

    # Check detail-toggle buttons
    print("\n=== DETAIL TOGGLES ===")
    toggles = page.locator(".detail-toggle")
    for i in range(toggles.count()):
        t = toggles.nth(i)
        box = t.bounding_box()
        text = t.inner_text()
        collapsed = "collapsed" in (t.get_attribute("class") or "")
        sibling_collapsed = t.evaluate("el => el.nextElementSibling?.classList.contains('collapsed')")
        print(f"  [{i}] '{text}' collapsed={collapsed} body_collapsed={sibling_collapsed} y={box['y']:.0f}")

    # Check hbar-wrap overlap with siblings
    print("\n=== HBAR-WRAP SPACING ===")
    hbars = page.locator(".hbar-wrap")
    for i in range(hbars.count()):
        hbar = hbars.nth(i)
        hbar_id = hbar.get_attribute("id") or ""
        box = hbar.bounding_box()
        if box:
            # Check next sibling
            next_y = hbar.evaluate("el => { const n = el.nextElementSibling; return n ? n.getBoundingClientRect().top : -1; }")
            gap = next_y - (box["y"] + box["height"]) if next_y > 0 else -1
            print(f"  [{i}] id={hbar_id} bottom={box['y']+box['height']:.0f} next_top={next_y:.0f} gap={gap:.0f}px")
            if 0 <= gap < 4:
                print(f"  ⚠ TOO TIGHT: only {gap:.0f}px gap")

    # Check metric sparklines presence
    print("\n=== SPARKLINES ===")
    sparks = page.locator(".metric-spark")
    print(f"  Found {sparks.count()} sparkline containers")
    for i in range(sparks.count()):
        s = sparks.nth(i)
        has_svg = s.locator("svg").count() > 0
        box = s.bounding_box()
        print(f"  [{i}] has_svg={has_svg} h={box['height']:.0f}" if box else f"  [{i}] has_svg={has_svg} no box")

    # Check hero layout
    print("\n=== HERO ===")
    hero_main = page.locator(".hero-main").first
    hero_box = hero_main.bounding_box()
    if hero_box:
        print(f"  hero-main: w={hero_box['width']:.0f} h={hero_box['height']:.0f}")
    hero_side = page.locator(".hero-side").first
    if hero_side.count():
        side_box = hero_side.bounding_box()
        if side_box:
            print(f"  hero-side: w={side_box['width']:.0f} h={side_box['height']:.0f}")
            if side_box["width"] > 10:
                print(f"  ⚠ hero-side still visible!")
        else:
            print(f"  hero-side: no bounding box (hidden)")
    else:
        print(f"  hero-side: not in DOM")

    page.close()
    browser.close()
    print("\nDone.")
