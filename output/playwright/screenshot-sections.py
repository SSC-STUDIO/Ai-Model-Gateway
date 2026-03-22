from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)

    # Overview page - multiple viewport sections
    page = browser.new_page(viewport={"width": 1440, "height": 900})
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(3000)

    # Top section (hero + topbar)
    page.screenshot(path="output/playwright/overview-top.png", clip={"x": 0, "y": 0, "width": 1440, "height": 900})
    print("Saved overview-top.png")

    # Scroll to performance + runtime cards
    page.evaluate("window.scrollTo(0, 400)")
    page.wait_for_timeout(500)
    page.screenshot(path="output/playwright/overview-perf.png", clip={"x": 0, "y": 0, "width": 1440, "height": 900})
    print("Saved overview-perf.png")

    # Scroll to upstream + errors
    page.evaluate("window.scrollTo(0, 900)")
    page.wait_for_timeout(500)
    page.screenshot(path="output/playwright/overview-upstream.png", clip={"x": 0, "y": 0, "width": 1440, "height": 900})
    print("Saved overview-upstream.png")

    # Scroll to charts
    page.evaluate("window.scrollTo(0, 1800)")
    page.wait_for_timeout(500)
    page.screenshot(path="output/playwright/overview-charts.png", clip={"x": 0, "y": 0, "width": 1440, "height": 900})
    print("Saved overview-charts.png")

    # Scroll to economics
    page.evaluate("window.scrollTo(0, 2600)")
    page.wait_for_timeout(500)
    page.screenshot(path="output/playwright/overview-economics.png", clip={"x": 0, "y": 0, "width": 1440, "height": 900})
    print("Saved overview-economics.png")

    page.close()

    # Settings page
    page2 = browser.new_page(viewport={"width": 1440, "height": 900})
    page2.goto("http://127.0.0.1:18080/admin/settings?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page2.wait_for_load_state("networkidle")
    page2.wait_for_timeout(3000)

    # Settings top
    page2.screenshot(path="output/playwright/settings-top.png", clip={"x": 0, "y": 0, "width": 1440, "height": 900})
    print("Saved settings-top.png")

    # Settings scroll to providers
    page2.evaluate("window.scrollTo(0, 800)")
    page2.wait_for_timeout(500)
    page2.screenshot(path="output/playwright/settings-mid.png", clip={"x": 0, "y": 0, "width": 1440, "height": 900})
    print("Saved settings-mid.png")

    # Settings scroll to bottom
    page2.evaluate("window.scrollTo(0, 1600)")
    page2.wait_for_timeout(500)
    page2.screenshot(path="output/playwright/settings-bottom.png", clip={"x": 0, "y": 0, "width": 1440, "height": 900})
    print("Saved settings-bottom.png")

    page2.close()
    browser.close()
