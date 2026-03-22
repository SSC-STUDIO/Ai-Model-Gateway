from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 5000})
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(4000)

    # Check paired cards - content bottom positions
    pairs = [
        ("upstreams-card", "errors-card", "Upstream Health vs Recent Errors"),
        ("economics", "cost-card", "Model Economics vs Cost Snapshot"),
        ("usage-card", "cache-card", "Upstream Usage vs Cache Hit Ranking"),
    ]

    for left_id, right_id, label in pairs:
        print(f"\n=== {label} ===")
        left = page.locator(f"#{left_id}")
        right = page.locator(f"#{right_id}")
        left_box = left.bounding_box()
        right_box = right.bounding_box()
        if left_box and right_box:
            print(f"  {left_id}: y={left_box['y']:.0f} h={left_box['height']:.0f} bottom={left_box['y']+left_box['height']:.0f}")
            print(f"  {right_id}: y={right_box['y']:.0f} h={right_box['height']:.0f} bottom={right_box['y']+right_box['height']:.0f}")

        # Check last child content position in each card
        for card_id in [left_id, right_id]:
            card = page.locator(f"#{card_id}")
            # Get all direct children bounding boxes
            children = card.evaluate("""el => {
                const kids = Array.from(el.children);
                return kids.map((k, i) => ({
                    index: i,
                    tag: k.tagName,
                    id: k.id || '',
                    cls: (k.className || '').substring(0, 40),
                    y: k.getBoundingClientRect().top,
                    h: k.getBoundingClientRect().height,
                    bottom: k.getBoundingClientRect().bottom,
                    text: (k.textContent || '').substring(0, 30).replace(/\\n/g, ' ')
                }));
            }""")
            print(f"\n  {card_id} children:")
            for c in children:
                print(f"    [{c['index']}] {c['tag']:8s} id={c['id']:20s} cls={c['cls']:30s} y={c['y']:.0f} h={c['h']:.0f} bottom={c['bottom']:.0f}")

    # Also check chart cards
    print(f"\n=== Chart Row 2: Success/Failure vs Token Usage ===")
    chart_layout = page.locator("#chartLayout")
    chart_cards = chart_layout.locator("> .card")
    for i in range(chart_cards.count()):
        card = chart_cards.nth(i)
        box = card.bounding_box()
        if box:
            # Get last visible content bottom
            last_content = card.evaluate("""el => {
                const kids = Array.from(el.children).filter(k => k.offsetHeight > 0);
                const last = kids[kids.length - 1];
                return last ? {
                    cls: last.className,
                    bottom: last.getBoundingClientRect().bottom,
                    cardBottom: el.getBoundingClientRect().bottom,
                    padding: el.getBoundingClientRect().bottom - last.getBoundingClientRect().bottom
                } : null;
            }""")
            print(f"  Chart [{i}] y={box['y']:.0f} h={box['height']:.0f} bottom={box['y']+box['height']:.0f} content_padding={last_content['padding']:.0f}px" if last_content else f"  Chart [{i}] y={box['y']:.0f} h={box['height']:.0f}")

    browser.close()
    print("\nDone.")
