from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 900})

    # Screenshot admin overview
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(2000)
    page.screenshot(path="output/playwright/admin-overview.png", full_page=True)
    print("Saved admin-overview.png")

    # Screenshot admin settings
    page.goto("http://127.0.0.1:18080/admin/settings?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(2000)
    page.screenshot(path="output/playwright/admin-settings.png", full_page=True)
    print("Saved admin-settings.png")

    browser.close()
