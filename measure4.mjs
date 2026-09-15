import { chromium } from "playwright";
const browser = await chromium.launch({ executablePath: "/usr/bin/chromium-browser", args: ["--no-sandbox"] });
const page = await browser.newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
await page.goto("http://127.0.0.1:18082/", { waitUntil: "domcontentloaded" });
await page.waitForSelector("[data-testid=chart]");
await page.waitForTimeout(800);
const chain = await page.evaluate(() => {
  const out = [];
  let element = document.querySelector("[data-testid=chart]");
  while (element && element !== document.body) {
    const style = getComputedStyle(element);
    out.push(`${element.tagName}.${String(element.className).slice(0, 30)} client=${element.clientWidth} display=${style.display} w=${style.width}`);
    element = element.parentElement;
  }
  return out;
});
console.log(chain.join("\n"));
await browser.close();
