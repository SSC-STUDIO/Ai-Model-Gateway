from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 900})
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(4000)

    # Full page screenshot
    page.screenshot(path="output/playwright/full-overview.png", full_page=True)
    print("Saved full-overview.png")

    # Section by section with precise clips
    # 1. Hero + topbar
    page.screenshot(path="output/playwright/v2-hero.png", clip={"x": 0, "y": 0, "width": 1440, "height": 400})
    print("1. hero")

    # 2. Performance + Runtime row
    page.evaluate("document.getElementById('performance').scrollIntoView({block:'start'})")
    page.wait_for_timeout(300)
    box = page.locator("#performance").bounding_box()
    if box:
        y = max(0, box["y"] - 5)
        page.screenshot(path="output/playwright/v2-perf.png", clip={"x": 0, "y": y, "width": 1440, "height": 450})
        print(f"2. perf y={y:.0f}")

    # 3. Upstream + Errors row
    page.evaluate("document.getElementById('upstreams-card').scrollIntoView({block:'start'})")
    page.wait_for_timeout(300)
    box = page.locator("#upstreams-card").bounding_box()
    if box:
        y = max(0, box["y"] - 5)
        page.screenshot(path="output/playwright/v2-upstream.png", clip={"x": 0, "y": y, "width": 1440, "height": 500})
        print(f"3. upstream y={y:.0f}")

    # 4. Requests
    page.evaluate("document.getElementById('requests-card').scrollIntoView({block:'start'})")
    page.wait_for_timeout(300)
    box = page.locator("#requests-card").bounding_box()
    if box:
        y = max(0, box["y"] - 5)
        page.screenshot(path="output/playwright/v2-requests.png", clip={"x": 0, "y": y, "width": 1440, "height": 600})
        print(f"4. requests y={y:.0f}")

    # 5. Charts
    page.evaluate("document.getElementById('chartLayout').scrollIntoView({block:'start'})")
    page.wait_for_timeout(300)
    box = page.locator("#chartLayout").bounding_box()
    if box:
        y = max(0, box["y"] - 5)
        page.screenshot(path="output/playwright/v2-charts.png", clip={"x": 0, "y": y, "width": 1440, "height": 600})
        print(f"5. charts y={y:.0f}")

    # 6. Economics (model + cost)
    page.evaluate("document.getElementById('economics').scrollIntoView({block:'start'})")
    page.wait_for_timeout(300)
    box = page.locator("#economics").bounding_box()
    if box:
        y = max(0, box["y"] - 5)
        page.screenshot(path="output/playwright/v2-economics.png", clip={"x": 0, "y": y, "width": 1440, "height": 500})
        print(f"6. economics y={y:.0f}")

    # 7. Usage + Cache
    page.evaluate("document.getElementById('usage-card').scrollIntoView({block:'start'})")
    page.wait_for_timeout(300)
    box = page.locator("#usage-card").bounding_box()
    if box:
        y = max(0, box["y"] - 5)
        page.screenshot(path="output/playwright/v2-usage.png", clip={"x": 0, "y": y, "width": 1440, "height": 500})
        print(f"7. usage y={y:.0f}")

    page.close()
    browser.close()
    print("Done.")
