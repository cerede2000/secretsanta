import type { FullResult, Reporter, TestCase, TestResult } from '@playwright/test/reporter';

// Garde du banc : un test « sauté » (test.skip, fixme, absence de prérequis) est
// un test qui ne prouve rien. Ce reporter fait échouer l'exécution entière dès
// qu'un test est sauté et nomme chacun d'eux. scripts/e2e.sh recoupe avec le
// rapport JSON (_shared/check-report.mts).
class NoSkipReporter implements Reporter {
  private skipped: string[] = [];

  onTestEnd(test: TestCase, result: TestResult): void {
    if (result.status === 'skipped' || test.expectedStatus === 'skipped') {
      this.skipped.push(`[${test.parent.project()?.name}] ${test.titlePath().slice(2).join(' › ')}`);
    }
  }

  onEnd(): Promise<{ status: FullResult['status'] } | undefined> | undefined {
    if (this.skipped.length === 0) return undefined;
    console.error(`\nGARDE DU BANC : ${this.skipped.length} test(s) sauté(s), interdit :`);
    for (const s of this.skipped) console.error(`  - ${s}`);
    return Promise.resolve({ status: 'failed' });
  }

  printsToStdio(): boolean {
    return false;
  }
}

export default NoSkipReporter;
