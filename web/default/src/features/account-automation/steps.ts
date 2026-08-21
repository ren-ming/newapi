/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

export type StepState = 'done' | 'active' | 'error' | 'pending'

export interface PipelineStep {
  /** Stable step identifier: submit | sms688 | credential | channel | test */
  key: string
  /** i18n key translating to the human step name */
  labelKey: string
  state: StepState
}

export interface StepJob {
  status: string
  error_class?: string
}

export const STEP_LABEL_KEYS = [
  'Submit to SMS688',
  'SMS688 processing',
  'Credential match',
  'Write to channel',
  'Test & enable',
] as const

/** Which pipeline step each in-flight status currently sits on. */
const ACTIVE_STEP_BY_STATUS: Record<string, number> = {
  submitting: 0,
  sms688_queued: 1,
  sms688_running: 1,
  sms688_waiting: 1,
  credential_ready: 2,
  channel_updated: 3,
  testing: 4,
}

/** Which pipeline step each terminal failure marks as failed. */
const ERROR_STEP_BY_STATUS: Record<string, number> = {
  submit_failed: 0,
  sms688_failed: 1,
  sms688_expired: 1,
  sms688_cancelled: 1,
  download_failed: 2,
  credential_invalid: 2,
  channel_update_failed: 3,
  channel_test_failed: 4,
}

/** Chinese-friendly label per job status (i18n keys, English source strings). */
export const STATUS_LABEL_KEYS: Record<string, string> = {
  submitting: 'Submitting task',
  sms688_queued: 'Queued at SMS688',
  sms688_running: 'SMS688 is processing',
  sms688_waiting: 'Waiting at SMS688',
  credential_ready: 'Matching credential',
  channel_updated: 'Writing to channel',
  testing: 'Testing & enabling channel',
  succeeded: 'Succeeded',
  submit_failed: 'Submit failed',
  sms688_failed: 'SMS688 failed',
  sms688_expired: 'SMS688 expired',
  sms688_cancelled: 'SMS688 cancelled',
  download_failed: 'Download failed',
  credential_invalid: 'Credential invalid',
  channel_update_failed: 'Channel update failed',
  channel_test_failed: 'Channel test failed',
}

/**
 * Human explanation per error class. Unknown classes fall back to the raw
 * identifier so no failure is ever hidden from the admin.
 */
export const ERROR_MESSAGE_KEYS: Record<string, string> = {
  sms688_submit_failed:
    'Failed to submit the task to SMS688 (network error or the account format was rejected)',
  sms688_transport_error:
    'Could not reach SMS688 over the network (check the server egress)',
  sms688_invalid_response:
    'SMS688 returned data that could not be parsed (the platform response may have changed)',
  sms688_poll_failed: 'Failed to query the SMS688 task status',
  sms688_poll_timeout:
    'SMS688 processing timed out (exceeded the 45-minute deadline)',
  sms688_job_missing:
    'The SMS688 batch finished but no task for this account was found',
  sms688_failed:
    'SMS688 failed to process this account (the platform could not produce a credential)',
  sms688_expired: 'The SMS688 task expired before finishing',
  sms688_cancelled: 'The SMS688 task was cancelled',
  download_failed: 'Failed to download the CPA credential archive',
  credential_invalid:
    'Credential match failed (no credential for this email in the CPA archive, or duplicates)',
  channel_update_failed:
    'Failed to write the credential into the channel (NewAPI rejected the update)',
  newapi_channel_update_failed:
    'Failed to persist the channel update (database write failed)',
  channel_test_failed:
    'Channel test failed (the upstream rejected the test request; usually an invalid credential or risk control)',
  newapi_channel_enable_failed:
    'Channel enable failed (the test passed but the channel could not be switched to enabled)',
  interrupted:
    'The job was interrupted by a service restart and cannot be resumed',
}

/**
 * Derives the state of every pipeline step from the job's current status.
 * Steps before the current position are done, the current one is active (or
 * error on terminal failure), the rest stay pending.
 */
export function deriveSteps(job: StepJob): PipelineStep[] {
  const errorIndex = ERROR_STEP_BY_STATUS[job.status]
  const activeIndex = ACTIVE_STEP_BY_STATUS[job.status]
  const succeeded = job.status === 'succeeded'

  return STEP_LABEL_KEYS.map((labelKey, index) => {
    if (errorIndex !== undefined) {
      if (index < errorIndex) return { key: STEP_KEYS[index], labelKey, state: 'done' as const }
      if (index === errorIndex) return { key: STEP_KEYS[index], labelKey, state: 'error' as const }
      return { key: STEP_KEYS[index], labelKey, state: 'pending' as const }
    }
    if (succeeded || (activeIndex !== undefined && index < activeIndex)) {
      return { key: STEP_KEYS[index], labelKey, state: 'done' as const }
    }
    if (activeIndex === index) {
      return { key: STEP_KEYS[index], labelKey, state: 'active' as const }
    }
    return { key: STEP_KEYS[index], labelKey, state: 'pending' as const }
  })
}

const STEP_KEYS = ['submit', 'sms688', 'credential', 'channel', 'test']
