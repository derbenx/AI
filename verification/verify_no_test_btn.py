import os
from playwright.sync_api import sync_playwright

def verify_frontend():
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()

        current_dir = os.getcwd()
        page.goto(f"file://{current_dir}/frontend/index.html")

        # Manually trigger the tab show logic
        page.evaluate("document.querySelectorAll('.tab-content').forEach(tab => tab.classList.remove('active'))")
        page.evaluate("document.getElementById('code-tab').classList.add('active')")

        # Take a screenshot of the Code tab to verify 'Test Mode' button is gone
        page.screenshot(path="verification/code_tab_no_test_btn.png")

        # Verify Test Mode button is gone
        btn = page.locator("#code-test-btn")
        if btn.count() == 0:
            print("Test Mode button successfully removed")
        else:
            print("Test Mode button STILL present")

        browser.close()

if __name__ == "__main__":
    verify_frontend()
