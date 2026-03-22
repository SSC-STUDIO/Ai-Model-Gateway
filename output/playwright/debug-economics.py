from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 900})
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(4000)

    # Check all cards in economics layout
    econ_layout = page.locator(".overview-economics")
    cards = econ_layout.locator("> .card")
    for i in range(cards.count()):
        card = cards.nth(i)
        box = card.bounding_box()
        card_id = card.get_attribute("id") or ""
        card_cls = card.get_attribute("class") or ""
        span = "span-8" if "span-8" in card_cls else "span-7" if "span-7" in card_cls else "span-5" if "span-5" in card_cls else "span-4" if "span-4" in card_cls else "span-12"
        print(f"Card [{i}] id={card_id:15s} {span:8s} x={box['x']:.0f} y={box['y']:.0f} w={box['width']:.0f} h={box['height']:.0f}")

    # Check computed grid properties
    grid_info = econ_layout.evaluate("""el => {
        const style = getComputedStyle(el);
        return {
            gridTemplateColumns: style.gridTemplateColumns,
            gridTemplateRows: style.gridTemplateRows,
            gap: style.gap,
            alignItems: style.alignItems
        };
    }""")
    print(f"\nGrid: {grid_info}")

    # Check if economics card has margin/padding issues
    econ = page.locator("#economics")
    econ_info = econ.evaluate("""el => {
        const style = getComputedStyle(el);
        return {
            gridColumn: style.gridColumn,
            gridRow: style.gridRow,
            marginTop: style.marginTop,
            marginBottom: style.marginBottom
        };
    }""")
    print(f"Economics grid: {econ_info}")

    cost = page.locator("#cost-card")
    cost_info = cost.evaluate("""el => {
        const style = getComputedStyle(el);
        return {
            gridColumn: style.gridColumn,
            gridRow: style.gridRow,
            marginTop: style.marginTop,
            marginBottom: style.marginBottom
        };
    }""")
    print(f"Cost-card grid: {cost_info}")

    browser.close()
