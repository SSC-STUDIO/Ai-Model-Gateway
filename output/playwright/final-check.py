from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 8000})
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(4000)

    pairs = [
        ("performance", "runtime-card"),
        ("upstreams-card", "errors-card"),
        ("economics", "cost-card"),
        ("usage-card", "cache-card"),
    ]
    print("=== CARD PAIRS (no scroll, 8000px viewport) ===")
    all_ok = True
    for left_id, right_id in pairs:
        l = page.locator(f"#{left_id}").bounding_box()
        r = page.locator(f"#{right_id}").bounding_box()
        if l and r:
            y_diff = abs(l["y"] - r["y"])
            h_diff = abs(l["height"] - r["height"])
            b_diff = abs((l["y"]+l["height"]) - (r["y"]+r["height"]))
            status = "✓" if h_diff < 2 and y_diff < 2 else "⚠"
            if status == "⚠": all_ok = False
            print(f"  {status} {left_id:20s} h={l['height']:.0f}  {right_id:20s} h={r['height']:.0f}  top_diff={y_diff:.0f} height_diff={h_diff:.0f} bottom_diff={b_diff:.0f}")

    # Check content bottom padding in each card
    print("\n=== CONTENT BOTTOM PADDING ===")
    for left_id, right_id in pairs:
        for card_id in [left_id, right_id]:
            card = page.locator(f"#{card_id}")
            info = card.evaluate("""el => {
                const rect = el.getBoundingClientRect();
                const kids = Array.from(el.children).filter(k => k.offsetHeight > 0);
                const last = kids[kids.length - 1];
                const lastBottom = last ? last.getBoundingClientRect().bottom : rect.top;
                return {
                    cardBottom: rect.bottom,
                    contentBottom: lastBottom,
                    padding: rect.bottom - lastBottom
                };
            }""")
            pad = info["padding"]
            flag = "⚠" if pad > 30 else "✓"
            print(f"  {flag} {card_id:20s} bottom_padding={pad:.0f}px")

    # Chart row content padding
    print("\n=== CHART BOTTOM PADDING ===")
    charts = page.locator("#chartLayout > .card")
    for i in range(charts.count()):
        card = charts.nth(i)
        info = card.evaluate("""el => {
            const rect = el.getBoundingClientRect();
            const kids = Array.from(el.children).filter(k => k.offsetHeight > 0);
            const last = kids[kids.length - 1];
            return { padding: rect.bottom - (last ? last.getBoundingClientRect().bottom : rect.top) };
        }""")
        print(f"  Chart [{i}] bottom_padding={info['padding']:.0f}px")

    if all_ok:
        print("\n✓ All card pairs aligned!")
    browser.close()
