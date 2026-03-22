from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)

    # Overview page - detailed sections
    page = browser.new_page(viewport={"width": 1440, "height": 900})
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(3000)

    # 1. Hero section close-up
    page.screenshot(path="output/playwright/detail-hero.png", clip={"x": 0, "y": 0, "width": 1440, "height": 500})

    # 2. Performance + Runtime cards side by side
    page.evaluate("document.getElementById('performance').scrollIntoView({block:'start'})")
    page.wait_for_timeout(500)
    perf_box = page.locator("#performance").bounding_box()
    if perf_box:
        page.screenshot(path="output/playwright/detail-perf-runtime.png", clip={"x": 0, "y": max(0, perf_box["y"] - 10), "width": 1440, "height": 500})

    # 3. Upstream + Errors cards side by side
    page.evaluate("document.getElementById('upstreams-card').scrollIntoView({block:'start'})")
    page.wait_for_timeout(500)
    up_box = page.locator("#upstreams-card").bounding_box()
    if up_box:
        page.screenshot(path="output/playwright/detail-upstream-errors.png", clip={"x": 0, "y": max(0, up_box["y"] - 10), "width": 1440, "height": 600})

    # 4. Charts row
    page.evaluate("document.getElementById('chartLayout').scrollIntoView({block:'start'})")
    page.wait_for_timeout(500)
    chart_box = page.locator("#chartLayout").bounding_box()
    if chart_box:
        page.screenshot(path="output/playwright/detail-charts.png", clip={"x": 0, "y": max(0, chart_box["y"] - 10), "width": 1440, "height": 600})

    # 5. Economics + Cost side by side
    page.evaluate("document.getElementById('economics').scrollIntoView({block:'start'})")
    page.wait_for_timeout(500)
    econ_box = page.locator("#economics").bounding_box()
    if econ_box:
        page.screenshot(path="output/playwright/detail-economics.png", clip={"x": 0, "y": max(0, econ_box["y"] - 10), "width": 1440, "height": 500})

    # 6. Usage + Cache side by side
    page.evaluate("document.getElementById('usage-card').scrollIntoView({block:'start'})")
    page.wait_for_timeout(500)
    usage_box = page.locator("#usage-card").bounding_box()
    if usage_box:
        page.screenshot(path="output/playwright/detail-usage-cache.png", clip={"x": 0, "y": max(0, usage_box["y"] - 10), "width": 1440, "height": 500})

    # 7. Requests card
    page.evaluate("document.getElementById('requests-card').scrollIntoView({block:'start'})")
    page.wait_for_timeout(500)
    req_box = page.locator("#requests-card").bounding_box()
    if req_box:
        page.screenshot(path="output/playwright/detail-requests.png", clip={"x": 0, "y": max(0, req_box["y"] - 10), "width": 1440, "height": 500})

    page.close()

    # Settings page - detailed sections
    page2 = browser.new_page(viewport={"width": 1440, "height": 900})
    page2.goto("http://127.0.0.1:18080/admin/settings?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page2.wait_for_load_state("networkidle")
    page2.wait_for_timeout(3000)

    # 8. Settings hero + nav + first config section
    page2.screenshot(path="output/playwright/detail-settings-hero.png", clip={"x": 0, "y": 0, "width": 1440, "height": 900})

    # 9. Settings nav panel close-up
    nav = page2.locator("#settingsNav").bounding_box()
    if nav:
        page2.screenshot(path="output/playwright/detail-settings-nav.png", clip={"x": max(0, nav["x"] - 5), "y": max(0, nav["y"] - 5), "width": min(300, 1440 - nav["x"]), "height": 500})

    # 10. Settings rail panel close-up
    rail_cards = page2.locator(".command-panel").bounding_box()
    if rail_cards:
        page2.screenshot(path="output/playwright/detail-settings-rail.png", clip={"x": max(0, rail_cards["x"] - 5), "y": max(0, rail_cards["y"] - 5), "width": min(400, 1440 - rail_cards["x"]), "height": 600})

    # 11. Config sections alignment
    page2.evaluate("document.getElementById('cfg-router').scrollIntoView({block:'start'})")
    page2.wait_for_timeout(500)
    router_box = page2.locator("#cfg-router").bounding_box()
    if router_box:
        page2.screenshot(path="output/playwright/detail-settings-router.png", clip={"x": 0, "y": max(0, router_box["y"] - 10), "width": 1440, "height": 900})

    # 12. Provider cards
    page2.evaluate("document.getElementById('cfg-upstreams').scrollIntoView({block:'start'})")
    page2.wait_for_timeout(500)
    ups_box = page2.locator("#cfg-upstreams").bounding_box()
    if ups_box:
        page2.screenshot(path="output/playwright/detail-settings-providers.png", clip={"x": 0, "y": max(0, ups_box["y"] - 10), "width": 1440, "height": 900})

    page2.close()
    browser.close()
    print("All detail screenshots saved.")
