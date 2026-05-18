const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();

  // Set viewport size
  await page.setViewportSize({ width: 1280, height: 720 });

  try {
    await page.goto('http://localhost:34115'); // Use the port from instructions or previous runs

    // Main Tab (Default)
    await page.screenshot({ path: '/home/jules/verification/main_tab_fixed.png' });
    console.log('Main tab screenshot saved.');

    // Switch to Code Tab
    await page.click('button:has-text("Code")');
    await page.waitForTimeout(500);
    await page.screenshot({ path: '/home/jules/verification/code_tab_fixed.png' });
    console.log('Code tab screenshot saved.');

    // Switch to AI Bots Tab
    await page.click('button:has-text("AI Bots")');
    await page.waitForTimeout(500);
    await page.screenshot({ path: '/home/jules/verification/bots_tab_fixed.png' });
    console.log('AI Bots tab screenshot saved.');

    // Switch to Settings Tab
    await page.click('button:has-text("Settings")');
    await page.waitForTimeout(500);
    await page.screenshot({ path: '/home/jules/verification/settings_tab_fixed.png' });
    console.log('Settings tab screenshot saved.');

  } catch (e) {
    console.error('Error during verification:', e);
  } finally {
    await browser.close();
  }
})();
