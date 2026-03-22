from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 5000})
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(4000)

    # Check layout computed styles
    layouts = page.locator(".layout")
    for i in range(layouts.count()):
        layout = layouts.nth(i)
        cls = layout.get_attribute("class") or ""
        info = layout.evaluate("""el => {
            const s = getComputedStyle(el);
            return { alignItems: s.alignItems, gridTemplateRows: s.gridTemplateRows, display: s.display };
        }""")
        print(f"Layout [{i}] '{cls}': {info}")

    # Check card computed styles for upstream/errors pair
    for card_id in ["upstreams-card", "errors-card"]:
        card = page.locator(f"#{card_id}")
        info = card.evaluate("""el => {
            const s = getComputedStyle(el);
            return { alignSelf: s.alignSelf, display: s.display, flexDirection: s.flexDirection, height: s.height, minHeight: s.minHeight };
        }""")
        box = card.bounding_box()
        print(f"  {card_id}: {info} actual_h={box['height']:.0f}")

    browser.close()
