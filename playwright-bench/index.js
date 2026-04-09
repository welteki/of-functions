'use strict';

const fs = require('fs');
const { chromium } = require('playwright');

// URLs chosen to represent a mix of page weight and complexity.
// Light: Hacker News, Medium: Wikipedia, Heavy: BBC News
const BENCHMARK_URLS = [
  'https://news.ycombinator.com',
  'https://www.wikipedia.org',
  'https://www.bbc.com',
];

// High-pressure URLs: compositor-heavy pages that stress /dev/shm via GPU tile
// buffers and large composited layer trees. Used when WORKLOAD=pressure.
const PRESSURE_URLS = [
  'https://www.youtube.com',         // video player, many composited layers
  'https://www.google.com/maps',     // WebGL canvas, heavy GPU tile usage
  'https://www.reddit.com',          // infinite scroll, many composited layers
  'https://twitter.com',             // heavy SPA, large composited DOM
  'https://www.bbc.com',             // mixed media, autoplaying video teasers
  'https://www.theverge.com',        // media-heavy, many images/video
  'https://news.ycombinator.com',    // filler: light, keeps tab count up
  'https://www.wikipedia.org',       // filler: medium, keeps tab count up
];

// PLAYWRIGHT_DATA_ON_SHM routes Chromium's user-data directory (profile,
// cache, etc.) into /dev/shm so that storage I/O also hits RAM.
const PLAYWRIGHT_DATA_ON_SHM = process.env.PLAYWRIGHT_DATA_ON_SHM === 'true';

// WORKLOAD selects which benchmark to run:
//   'standard'  -- 3 sequential URLs, screenshot + PDF (default)
//   'pressure'  -- many parallel tabs, scroll each page, keep all renderers
//                  alive simultaneously to stress /dev/shm IPC channels
const WORKLOAD = process.env.WORKLOAD || 'standard';

// TAB_COUNT controls how many tabs are opened in parallel in pressure mode.
// Each tab spawns a renderer process with its own /dev/shm IPC channel.
// Default 8 -- likely to exceed 64 MiB on GPU-heavy pages.
const TAB_COUNT = parseInt(process.env.TAB_COUNT || '8', 10);

// SCROLL_STEPS controls how many times each page is scrolled in pressure mode.
// Each scroll forces the compositor to generate new GPU tile buffers in /dev/shm.
const SCROLL_STEPS = parseInt(process.env.SCROLL_STEPS || '10', 10);

// Read the actual total size of /dev/shm from the kernel via statfs.
function getShmSizeBytes() {
  try {
    const stats = fs.statfsSync('/dev/shm');
    return stats.blocks * stats.bsize;
  } catch {
    return null;
  }
}

// Scroll a page in increments to force the compositor to allocate new GPU tile
// buffers for off-screen content. This is the main mechanism by which scrolling
// pushes additional data through /dev/shm.
async function scrollPage(page, steps) {
  for (let i = 0; i < steps; i++) {
    await page.evaluate(() => window.scrollBy(0, window.innerHeight));
    // Brief pause -- gives the compositor a chance to render newly visible tiles.
    await page.waitForTimeout(100);
  }
}

async function runStandard(context) {
  const tTabsStart = Date.now();
  const pages = await Promise.all(BENCHMARK_URLS.map(() => context.newPage()));
  const tabOpenMs = Date.now() - tTabsStart;

  const pageResults = [];

  for (let i = 0; i < BENCHMARK_URLS.length; i++) {
    const url = BENCHMARK_URLS[i];
    const page = pages[i];

    const tLoad = Date.now();
    await page.goto(url, { waitUntil: 'load', timeout: 60000 });
    const loadMs = Date.now() - tLoad;

    const tScreenshot = Date.now();
    await page.screenshot({ type: 'jpeg', quality: 80 });
    const screenshotMs = Date.now() - tScreenshot;

    const tPdf = Date.now();
    await page.pdf({ format: 'A4' });
    const pdfMs = Date.now() - tPdf;

    pageResults.push({ url, loadMs, screenshotMs, pdfMs });
  }

  return { tabOpenMs, pages: pageResults };
}

async function runPressure(context) {
  // Pick TAB_COUNT URLs, cycling through PRESSURE_URLS if needed.
  const urls = Array.from(
    { length: TAB_COUNT },
    (_, i) => PRESSURE_URLS[i % PRESSURE_URLS.length]
  );

  // Open all tabs in parallel -- this is the primary /dev/shm pressure point.
  // Each tab spawns a renderer process which immediately allocates an IPC
  // channel in /dev/shm. With many tabs alive simultaneously the 64 MiB
  // default can be exhausted.
  const tTabsStart = Date.now();
  const pages = await Promise.all(urls.map(() => context.newPage()));
  const tabOpenMs = Date.now() - tTabsStart;

  // Load all pages in parallel, keeping all renderers alive simultaneously.
  const tLoadStart = Date.now();
  const loadResults = await Promise.allSettled(
    pages.map((page, i) =>
      page.goto(urls[i], { waitUntil: 'load', timeout: 60000 })
    )
  );
  const parallelLoadMs = Date.now() - tLoadStart;

  // Scroll all successfully loaded pages in parallel.
  // Scrolling forces the compositor to allocate GPU tile buffers for newly
  // visible content, pushing additional data through /dev/shm.
  const tScrollStart = Date.now();
  await Promise.allSettled(
    pages.map((page, i) =>
      loadResults[i].status === 'fulfilled'
        ? scrollPage(page, SCROLL_STEPS)
        : Promise.resolve()
    )
  );
  const parallelScrollMs = Date.now() - tScrollStart;

  // Screenshot all pages in parallel while all renderers are still alive.
  const tScreenshotStart = Date.now();
  const screenshotResults = await Promise.allSettled(
    pages.map(page => page.screenshot({ type: 'jpeg', quality: 80 }))
  );
  const parallelScreenshotMs = Date.now() - tScreenshotStart;

  // Summarise per-tab outcomes.
  const tabResults = urls.map((url, i) => ({
    url,
    loaded: loadResults[i].status === 'fulfilled',
    loadError: loadResults[i].status === 'rejected'
      ? loadResults[i].reason?.message
      : null,
    screenshotOk: screenshotResults[i].status === 'fulfilled',
  }));

  const loadedCount = tabResults.filter(t => t.loaded).length;
  const crashedCount = tabResults.filter(t => !t.loaded).length;

  return {
    tabCount: TAB_COUNT,
    scrollSteps: SCROLL_STEPS,
    tabOpenMs,
    parallelLoadMs,
    parallelScrollMs,
    parallelScreenshotMs,
    loadedCount,
    crashedCount,
    tabs: tabResults,
  };
}

async function runBenchmark() {
  const launchArgs = [
    '--no-sandbox',
    '--disable-setuid-sandbox',
    '--disable-gpu',
  ];

  const tLaunchStart = Date.now();

  let browser, context;
  if (PLAYWRIGHT_DATA_ON_SHM) {
    context = await chromium.launchPersistentContext('/dev/shm/chromium-data', {
      args: launchArgs,
    });
    browser = context.browser();
  } else {
    browser = await chromium.launch({ args: launchArgs });
    context = await browser.newContext();
  }

  const browserLaunchMs = Date.now() - tLaunchStart;

  let workloadResult;
  if (WORKLOAD === 'pressure') {
    workloadResult = await runPressure(context);
  } else {
    workloadResult = await runStandard(context);
  }

  await context.close();
  if (browser) await browser.close();

  return {
    config: {
      workload: WORKLOAD,
      shmSizeBytes: getShmSizeBytes(),
      dataOnShm: PLAYWRIGHT_DATA_ON_SHM,
    },
    results: {
      browserLaunchMs,
      ...workloadResult,
    },
  };
}

// Classic watchdog protocol: the watchdog forks this process per request
// and pipes the HTTP request body to stdin. We write the response to stdout.
let body = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', (chunk) => { body += chunk; });
process.stdin.on('end', async () => {
  try {
    const result = await runBenchmark();
    process.stdout.write(JSON.stringify(result, null, 2) + '\n');
  } catch (err) {
    process.stderr.write((err.stack || err.message) + '\n');
    process.exitCode = 1;
  }
});
