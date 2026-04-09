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

// PLAYWRIGHT_DATA_ON_SHM routes Chromium's user-data directory (profile,
// cache, etc.) into /dev/shm so that storage I/O also hits RAM.
// Only meaningful when a large /dev/shm is mounted since 64Mi fills up quickly.
const PLAYWRIGHT_DATA_ON_SHM = process.env.PLAYWRIGHT_DATA_ON_SHM === 'true';

// Read the actual total size of /dev/shm from the kernel via statfs.
// Returns bytes, or null if the path is not accessible.
function getShmSizeBytes() {
  try {
    const stats = fs.statfsSync('/dev/shm');
    return stats.blocks * stats.bsize;
  } catch {
    return null;
  }
}

async function runBenchmark() {
  const launchArgs = [
    // Required when running as root inside a container.
    '--no-sandbox',
    '--disable-setuid-sandbox',
    // Disable GPU acceleration - not available in pods.
    '--disable-gpu',
  ];

  // --- Browser launch (measures cold-start / scale-from-zero cost) ---
  const tLaunchStart = Date.now();

  let browser, context;
  if (PLAYWRIGHT_DATA_ON_SHM) {
    // launchPersistentContext routes Chromium's profile and cache to /dev/shm
    // so all storage I/O goes through the RAM-backed volume instead of the
    // overlay filesystem. userDataDir must be passed here, not as a launch arg.
    context = await chromium.launchPersistentContext('/dev/shm/chromium-data', {
      args: launchArgs,
    });
    browser = context.browser();
  } else {
    browser = await chromium.launch({ args: launchArgs });
    context = await browser.newContext();
  }

  const browserLaunchMs = Date.now() - tLaunchStart;

  // --- Open all tabs in parallel (measures multi-tab init cost) ---
  const tTabsStart = Date.now();
  const pages = await Promise.all(BENCHMARK_URLS.map(() => context.newPage()));
  const tabOpenMs = Date.now() - tTabsStart;

  const pageResults = [];

  for (let i = 0; i < BENCHMARK_URLS.length; i++) {
    const url = BENCHMARK_URLS[i];
    const page = pages[i];

    // --- Page load ---
    const tLoad = Date.now();
    await page.goto(url, { waitUntil: 'load', timeout: 60000 });
    const loadMs = Date.now() - tLoad;

    // --- Screenshot ---
    const tScreenshot = Date.now();
    await page.screenshot({ type: 'jpeg', quality: 80 });
    const screenshotMs = Date.now() - tScreenshot;

    // --- PDF generation ---
    const tPdf = Date.now();
    await page.pdf({ format: 'A4' });
    const pdfMs = Date.now() - tPdf;

    pageResults.push({
      url,
      loadMs,
      screenshotMs,
      pdfMs,
    });
  }

  await context.close();
  if (browser) await browser.close();

  return {
    config: {
      shmSizeBytes: getShmSizeBytes(),
      dataOnShm: PLAYWRIGHT_DATA_ON_SHM,
    },
    results: {
      browserLaunchMs,
      tabOpenMs,
      pages: pageResults,
    },
  };
}

// Classic watchdog protocol: the watchdog forks this process per request
// and pipes the HTTP request body to stdin. We write the response to stdout.
// This means every invocation gets a fresh Chromium launch, which is exactly
// what we want so that browser launch time is included in each benchmark run.
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
