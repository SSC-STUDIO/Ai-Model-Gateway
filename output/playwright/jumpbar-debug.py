from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 5000})
    page.goto("http://127.0.0.1:18080/admin/settings?token=ec6a94485ddd476b96cdc3d5a9a9fe14")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(3000)

    # Check each jumpbar link's inner structure
    links = page.locator(".settings-jumpbar a")
    for i in range(links.count()):
        link = links.nth(i)
        box = link.bounding_box()
        strong = link.locator("strong").first
        em = link.locator("em").first
        strong_box = strong.bounding_box() if strong.count() else None
        em_box = em.bounding_box() if em.count() else None
        strong_text = strong.inner_text() if strong.count() else ""
        em_text = em.inner_text() if em.count() else ""

        print(f"Link [{i}] h={box['height']:.0f} '{strong_text}' | em='{em_text}'")
        if strong_box:
            print(f"  strong: x={strong_box['x']:.1f} y={strong_box['y']:.1f} w={strong_box['width']:.1f} h={strong_box['height']:.1f}")
        if em_box:
            print(f"  em:     x={em_box['x']:.1f} y={em_box['y']:.1f} w={em_box['width']:.1f} h={em_box['height']:.1f}")

        # Check the jumpbar-copy div
        copy_div = link.locator(".settings-jumpbar-copy").first
        if copy_div.count():
            copy_box = copy_div.bounding_box()
            print(f"  copy:   x={copy_box['x']:.1f} y={copy_box['y']:.1f} w={copy_box['width']:.1f} h={copy_box['height']:.1f}")
            # Check if there's a span inside
            span = copy_div.locator("span").first
            if span.count():
                span_box = span.bounding_box()
                span_text = span.inner_text()
                print(f"  span:   x={span_box['x']:.1f} y={span_box['y']:.1f} w={span_box['width']:.1f} h={span_box['height']:.1f} '{span_text}'")

    browser.close()
