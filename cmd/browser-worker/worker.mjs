import { chromium } from '@playwright/test';
import Redis from 'ioredis';
import { mkdir, stat } from 'node:fs/promises';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';

const redis = new Redis(process.env.REDIS_URL || 'redis://localhost:6379/0');
const queueKey = process.env.BROWSER_QUEUE || 'queue:browser';
const artifactDir = process.env.BROWSER_ARTIFACT_DIR || '/tmp/gouptime-browser-artifacts';

await mkdir(artifactDir, { recursive: true });

async function runJob(job) {
  const started = Date.now();
  const consoleErrors = [];
  const networkErrors = [];
  const artifacts = [];
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text());
  });
  page.on('requestfailed', (request) => {
    networkErrors.push(`${request.method()} ${request.url()} ${request.failure()?.errorText || ''}`.trim());
  });
  try {
    const fn = new Function('page', 'env', job.source);
    await Promise.race([
      fn(page, job.env || {}),
      new Promise((_, reject) => setTimeout(() => reject(new Error('browser script timed out')), job.timeoutMs || 30000)),
    ]);
    return {
      jobId: job.jobId,
      success: true,
      durationMs: Date.now() - started,
      consoleErrors,
      networkErrors,
      artifacts,
    };
  } catch (error) {
    const file = join(artifactDir, `${job.monitorId}-${randomUUID()}.png`);
    await page.screenshot({ path: file, fullPage: true }).catch(() => {});
    const info = await stat(file).catch(() => null);
    if (info) {
      artifacts.push({
        type: 'screenshot',
        path: file,
        sizeBytes: info.size,
        retentionDays: job.retentionDays || 7,
      });
    }
    return {
      jobId: job.jobId,
      success: false,
      durationMs: Date.now() - started,
      error: error?.message || String(error),
      consoleErrors,
      networkErrors,
      artifacts,
      screenshotUrl: file,
    };
  } finally {
    await browser.close().catch(() => {});
  }
}

for (;;) {
  const [, payload] = await redis.brpop(queueKey, 0);
  let job;
  try {
    job = JSON.parse(payload);
    const result = await runJob(job);
    await redis.publish(job.resultChannel || 'queue:browser:results', JSON.stringify(result));
  } catch (error) {
    if (job?.resultChannel && job?.jobId) {
      await redis.publish(job.resultChannel, JSON.stringify({
        jobId: job.jobId,
        success: false,
        durationMs: 0,
        error: error?.message || String(error),
      }));
    }
  }
}
