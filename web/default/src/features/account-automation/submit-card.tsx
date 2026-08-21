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
import { useState } from 'react'
import { Plus, Send, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import type { Channel } from '@/features/channels/types'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

const ACCOUNT_MODES = ['microsoft', 'totp'] as const
type AccountMode = (typeof ACCOUNT_MODES)[number]

const MODE_PLACEHOLDERS: Record<AccountMode, string> = {
  microsoft: 'user@example.com----password',
  totp: 'user@example.com----password----TOTP_SECRET',
}

export const modeLabel = (mode: string): string =>
  mode === 'microsoft' ? 'Microsoft' : mode === 'totp' ? 'TOTP' : mode

interface DraftRow {
  /** Stable row identity; never address rows by array index. */
  id: string
  accountMode: AccountMode
  accountText: string
  channelId: number | ''
}

/**
 * Row IDs only address rows in React state, so plain randomness is fine —
 * and unlike crypto.randomUUID() this works on insecure origins (the panel
 * is still served over plain HTTP).
 */
const newRowId = (): string =>
  `row-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`

const emptyDraft = (): DraftRow => ({
  id: newRowId(),
  accountMode: 'microsoft',
  accountText: '',
  channelId: '',
})

type RowIssue = 'missing-channel' | 'duplicate-channel'

const isFilled = (row: DraftRow): boolean => row.accountText.trim() !== ''

/**
 * Validates filled rows: every one needs a channel, and a channel may appear
 * in at most one row because a later job would overwrite the earlier job's
 * credential on the same channel.
 */
function validateDrafts(drafts: DraftRow[]): Map<string, RowIssue> {
  const issues = new Map<string, RowIssue>()
  const firstRowByChannel = new Map<number, string>()
  for (const row of drafts) {
    if (!isFilled(row)) {
      continue
    }
    if (row.channelId === '' || row.channelId <= 0) {
      issues.set(row.id, 'missing-channel')
      continue
    }
    const firstRowId = firstRowByChannel.get(row.channelId)
    if (firstRowId !== undefined) {
      issues.set(firstRowId, 'duplicate-channel')
      issues.set(row.id, 'duplicate-channel')
    } else {
      firstRowByChannel.set(row.channelId, row.id)
    }
  }
  return issues
}

export function AccountAutomationSubmitCard({
  channels,
  onSubmitted,
}: {
  channels: Channel[]
  onSubmitted: () => void | Promise<void>
}) {
  const { t } = useTranslation()
  const [drafts, setDrafts] = useState<DraftRow[]>([emptyDraft()])
  const [rowIssues, setRowIssues] = useState<Map<string, RowIssue>>(new Map())
  const [bindFree, setBindFree] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const updateDraft = (id: string, patch: Partial<DraftRow>) =>
    setDrafts((current) =>
      current.map((row) => (row.id === id ? { ...row, ...patch } : row))
    )

  const addDraft = () =>
    setDrafts((current) => [...current, emptyDraft()])

  const removeDraft = (id: string) =>
    setDrafts((current) =>
      current.length <= 1
        ? [emptyDraft()]
        : current.filter((row) => row.id !== id)
    )

  const filledCount = drafts.filter(isFilled).length

  const submit = async () => {
    const rows = drafts.filter(isFilled)
    if (rows.length === 0) {
      setError(t('Add at least one account'))
      return
    }
    const issues = validateDrafts(drafts)
    setRowIssues(issues)
    if (issues.size > 0) {
      setError(
        [...issues.values()].some((issue) => issue === 'duplicate-channel')
          ? t('Duplicate channel selection')
          : t('Select a channel for the filled rows')
      )
      return
    }

    setSubmitting(true)
    setError('')
    setNotice('')
    const submittedIds = new Set<string>()
    for (const row of rows) {
      try {
        await api.post('/api/account-automation/jobs', {
          account_mode: row.accountMode,
          account_text: row.accountText,
          channel_id: row.channelId,
          bind_free: bindFree,
        })
        submittedIds.add(row.id)
      } catch (submitError: unknown) {
        const message =
          (submitError as { response?: { data?: { error?: string } } })?.response
            ?.data?.error ?? String((submitError as Error).message)
        setError(message)
        break
      }
    }
    setSubmitting(false)

    if (submittedIds.size > 0) {
      setNotice(t('Submitted {{count}} accounts', { count: submittedIds.size }))
      await onSubmitted()
    }
    // Successful rows leave the list; failures and untouched rows stay put.
    setDrafts((current) => {
      const remaining = current.filter((row) => !submittedIds.has(row.id))
      return remaining.length > 0 ? remaining : [emptyDraft()]
    })
  }

  return (
    <Card className='mb-6'>
      <CardHeader>
        <CardTitle>{t('Submit accounts')}</CardTitle>
        <CardDescription>
          {t('One account = one job; history is kept in the database')}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-4'>
        {error ? (
          <Alert variant='destructive'>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        {notice ? (
          <Alert>
            <AlertDescription>{notice}</AlertDescription>
          </Alert>
        ) : null}

        <div className='space-y-2'>
          {drafts.map((row) => {
            const issue = rowIssues.get(row.id)
            return (
              <div
                key={row.id}
                className='grid gap-2 md:grid-cols-[8.5rem_minmax(0,1fr)_12rem_2.25rem] md:items-center'
              >
                <NativeSelect
                  aria-label={t('Account type')}
                  value={row.accountMode}
                  onChange={(event) =>
                    updateDraft(row.id, {
                      accountMode: event.target.value as AccountMode,
                    })
                  }
                  className='h-9 w-full'
                >
                  {ACCOUNT_MODES.map((mode) => (
                    <NativeSelectOption key={mode} value={mode}>
                      {t(modeLabel(mode))}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <Input
                  aria-label={t('Account')}
                  value={row.accountText}
                  onChange={(event) =>
                    updateDraft(row.id, { accountText: event.target.value })
                  }
                  placeholder={MODE_PLACEHOLDERS[row.accountMode]}
                  spellCheck={false}
                  autoComplete='off'
                  className={cn(
                    'h-9 font-mono text-sm',
                    issue && 'border-destructive focus-visible:ring-destructive'
                  )}
                />
                <NativeSelect
                  aria-label={t('Target channel')}
                  value={row.channelId === '' ? '' : String(row.channelId)}
                  onChange={(event) =>
                    updateDraft(row.id, {
                      channelId:
                        event.target.value === ''
                          ? ''
                          : Number(event.target.value),
                    })
                  }
                  className={cn(
                    'h-9 w-full',
                    issue && 'border-destructive focus-visible:ring-destructive'
                  )}
                >
                  {channels.length === 0 ? (
                    <NativeSelectOption value=''>
                      {t('No Codex channels')}
                    </NativeSelectOption>
                  ) : (
                    channels.map((channel) => (
                      <NativeSelectOption
                        key={channel.id}
                        value={String(channel.id)}
                      >
                        {`${channel.name} (#${channel.id})`}
                      </NativeSelectOption>
                    ))
                  )}
                </NativeSelect>
                <button
                  type='button'
                  aria-label={t('Remove row')}
                  title={t('Remove row')}
                  onClick={() => removeDraft(row.id)}
                  className='text-muted-foreground hover:text-destructive flex h-9 w-9 items-center justify-center rounded-md transition-colors hover:bg-muted md:justify-self-center'
                >
                  <X className='h-4 w-4' />
                </button>
              </div>
            )
          })}
        </div>

        <div className='flex flex-wrap items-center justify-between gap-3'>
          <Button variant='outline' onClick={addDraft}>
            <Plus className='h-4 w-4' />
            {t('Add row')}
          </Button>
          <div className='flex flex-wrap items-center gap-4'>
            <span className='text-muted-foreground text-xs'>
              {t('Ready to submit: {{count}}', { count: filledCount })}
            </span>
            <label className='text-muted-foreground flex cursor-pointer items-center gap-2 text-sm font-normal select-none'>
              <Switch
                checked={bindFree}
                onCheckedChange={(checked) => setBindFree(checked === true)}
              />
              {t('Bind free plan')}
            </label>
            <Button
              onClick={() => void submit()}
              disabled={
                submitting || filledCount === 0 || channels.length === 0
              }
              className='gap-2'
            >
              {submitting ? (
                <Spinner className='h-4 w-4' />
              ) : (
                <Send className='h-4 w-4' />
              )}
              {t('Submit')}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
