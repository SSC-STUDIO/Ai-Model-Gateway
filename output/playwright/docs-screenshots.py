from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)

    # Overview page
    page = browser.new_page(viewport={"width": 1440, "height": 900})
    page.goto("http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(4000)
    page.screenshot(path="docs/assets/admin-overview.png", full_page=True)
    print("Saved admin-overview.png")

    # Settings page
    page.goto("http://127.0.0.1:18080/admin/settings?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(4000)
    page.screenshot(path="docs/assets/admin-settings.png", full_page=True)
    print("Saved admin-settings.png")

    page.close()
    browser.close()
    print("Done.")
