import os
from playwright.sync_api import sync_playwright

def verify_frontend():
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()

        current_dir = os.getcwd()
        page.goto(f"file://{current_dir}/frontend/index.html")

        # Manually trigger the tab show logic since we're loading statically
        page.evaluate("document.querySelectorAll('.tab-content').forEach(tab => tab.classList.remove('active'))")
        page.evaluate("document.getElementById('settings-tab').classList.add('active')")

        # Force the remote-only elements to show
        page.evaluate("document.querySelectorAll('.remote-only').forEach(el => el.style.display = 'block')")

        # Take a screenshot
        page.screenshot(path="verification/settings_remote_notice.png", full_page=True)

        # Check if the notice is visible
        notice = page.locator("text=Gemma 4 / Jinja Notice")
        if notice.is_visible():
            print("Notice is visible")
        else:
            print("Notice NOT visible")

        browser.close()

if __name__ == "__main__":
    verify_frontend()
