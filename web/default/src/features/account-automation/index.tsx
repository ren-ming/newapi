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
import { Fragment, useCallback, useEffect, useState } from 'react'
import {
  ChevronDown,
  ChevronRight,
  KeyRound,
  Mail,
  Send,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { getChannels } from '@/features/channels/api'
import type { Channel } from '@/features/channels/types'
import { SectionPageLayout } from '@/components/layout'
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
import { DetailStepper, MiniStepper } from './stepper'
import {
  deriveSteps,
  ERROR_MESSAGE_KEYS,
  STATUS_LABEL_KEYS,
  STEP_LABEL_KEYS,
} from './steps'

const PAGE_SIZE = 20
const CODEX_CHANNEL_TYPE = 57

const ACCOUNT_MODES = ['microsoft', 'totp'] as const
type AccountMode = (typeof ACCOUNT_MODES)[number]

const MODE_PLACEHOLDERS: Record<AccountMode, string> = {
  microsoft: 'user@example.com----password',
  totp: 'user@example.com----password----TOTP_SECRET',
}

const MODE_ICONS: Record<AccountMode, typeof Mail> = {
  microsoft: Mail,
  totp: ShieldCheck,
}

/** Credential format tokens shown as chips below the account input. */
const FORMAT_SEGMENTS: Record<
  AccountMode,
  Array<{ token: string; optional?: boolean }>
> = {
  microsoft: [
    { token: 'email' },
    { token: 'password' },
    { token: 'client_id', optional: true },
    { token: 'refresh_token', optional: true },
  ],
  totp: [
    { token: 'email' },
    { token: 'password' },
    { token: 'TOTP_SECRET' },
  ],
}

const MICRO_LABEL =
  'text-muted-foreground text-[11px] font-medium tracking-wider uppercase'

const modeLabel = (mode: string): string =>
  mode === 'microsoft' ? 'Microsoft' : mode === 'totp' ? 'TOTP' : mode

interface AutomationJob {
  id: string
  account_mode: string
  masked_email: string
  channel_id: number
  status: string
  stage?: string
  error_class?: string
  sms688_batch_id?: string
  created_at: string
  updated_at: string
}

interface JobListResponse {
  jobs: AutomationJob[]
  total: number
}

const errorText = (job: AutomationJob): string =>
  job.error_class
    ? ERROR_MESSAGE_KEYS[job.error_class] ?? job.error_class
    : ''

const formatTime = (value: string): string => {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

function AccountAutomationContent() {
  const { t } = useTranslation()
  const [accountMode, setAccountMode] = useState<AccountMode>('microsoft')
  const [accountText, setAccountText] = useState('')
  const [channelId, setChannelId] = useState<number | ''>('')
  const [channels, setChannels] = useState<Channel[]>([])
  const [bindFree, setBindFree] = useState(false)
  const [jobs, setJobs] = useState<AutomationJob[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const toggleExpanded = (id: string) =>
    setExpandedId((current) => (current === id ? null : id))

  const loadChannels = useCallback(async () => {
    try {
      const data = await getChannels({ type: CODEX_CHANNEL_TYPE, page_size: 100 })
      const items = data.data?.items ?? []
      setChannels(items)
      setChannelId((current) =>
        current === '' && items.length > 0 ? items[0].id : current
      )
    } catch {
      // Channel loading failures leave the selector empty; the admin can retry
      // by reopening the page.
    }
  }, [])

  const refresh = useCallback(
    async (targetPage: number) => {
      try {
        const { data } = await api.get<JobListResponse>(
          '/api/account-automation/jobs',
          { params: { offset: targetPage * PAGE_SIZE, limit: PAGE_SIZE } }
        )
        setJobs(Array.isArray(data.jobs) ? data.jobs : [])
        setTotal(typeof data.total === 'number' ? data.total : 0)
      } catch {
        // Polling failures are silent; the next tick retries.
      }
    },
    []
  )

  useEffect(() => {
    void loadChannels()
  }, [loadChannels])

  useEffect(() => {
    void refresh(page)
    const timer = setInterval(() => void refresh(page), 2000)
    return () => clearInterval(timer)
  }, [refresh, page])

  const submit = async () => {
    if (channelId === '' || channelId <= 0) {
      setError(t('Select a Codex channel first'))
      return
    }
    setSubmitting(true)
    setError('')
    setNotice('')
    try {
      await api.post('/api/account-automation/jobs', {
        account_mode: accountMode,
        account_text: accountText,
        channel_id: channelId,
        bind_free: bindFree,
      })
      setAccountText('')
      setNotice(t('Job submitted'))
      setPage(0)
      await refresh(0)
    } catch (submitError: unknown) {
      const message =
        (submitError as { response?: { data?: { error?: string } } })?.response
          ?.data?.error ?? (submitError as Error).message
      setError(String(message))
    } finally {
      setSubmitting(false)
    }
  }

  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Account Automation')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'Submit one account per job; the CPA credential is written into the selected Codex channel automatically'
        )}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        {error ? (
          <Alert variant='destructive' className='mb-4'>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        {notice ? (
          <Alert className='mb-4'>
            <AlertDescription>{notice}</AlertDescription>
          </Alert>
        ) : null}

        <Card className='mb-6'>
          <CardHeader>
            <CardTitle>{t('Submit accounts')}</CardTitle>
            <CardDescription>
              {t('One account = one job; history is kept in the database')}
            </CardDescription>
          </CardHeader>
          <CardContent className='space-y-5'>
            <div
              role='radiogroup'
              aria-label={t('Account type')}
              className='bg-muted/60 inline-flex w-full gap-0.5 rounded-lg p-0.5 sm:w-fit'
            >
              {ACCOUNT_MODES.map((mode) => {
                const Icon = MODE_ICONS[mode]
                const active = accountMode === mode
                return (
                  <button
                    key={mode}
                    type='button'
                    role='radio'
                    aria-checked={active}
                    onClick={() => setAccountMode(mode)}
                    className={cn(
                      'flex flex-1 items-center justify-center gap-1.5 rounded-md px-4 py-2 text-sm font-medium transition-all sm:flex-none',
                      active
                        ? 'bg-background text-foreground shadow-sm'
                        : 'text-muted-foreground hover:text-foreground'
                    )}
                  >
                    <Icon
                      className={cn(
                        'h-4 w-4',
                        active ? 'text-primary' : 'text-muted-foreground/70'
                      )}
                    />
                    {t(modeLabel(mode))}
                  </button>
                )
              })}
            </div>

            <div className='space-y-2'>
              <label className={MICRO_LABEL} htmlFor='account-text'>
                {t('Account')}
              </label>
              <div className='relative'>
                <KeyRound className='pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground/60' />
                <Input
                  id='account-text'
                  value={accountText}
                  onChange={(event) => setAccountText(event.target.value)}
                  placeholder={MODE_PLACEHOLDERS[accountMode]}
                  spellCheck={false}
                  autoComplete='off'
                  className='h-11 pr-3 pl-9 font-mono text-sm'
                />
              </div>
              <div className='flex flex-wrap items-center gap-1 leading-none'>
                {FORMAT_SEGMENTS[accountMode].map((segment, index) => (
                  <Fragment key={segment.token}>
                    {index > 0 ? (
                      <span className='font-mono text-[11px] text-muted-foreground/50'>
                        ----
                      </span>
                    ) : null}
                    <span
                      className={cn(
                        'bg-muted text-muted-foreground rounded px-1.5 py-1 font-mono text-[11px]',
                        segment.optional &&
                          'border-border bg-transparent border-dashed opacity-60'
                      )}
                    >
                      {segment.token}
                    </span>
                  </Fragment>
                ))}
              </div>
            </div>

            <div className='grid gap-5 sm:grid-cols-[1fr_auto] sm:items-end'>
              <div className='space-y-2'>
                <label className={MICRO_LABEL} htmlFor='account-channel'>
                  {t('Target channel')}
                </label>
                <NativeSelect
                  id='account-channel'
                  value={channelId === '' ? '' : String(channelId)}
                  onChange={(event) =>
                    setChannelId(
                      event.target.value === '' ? '' : Number(event.target.value)
                    )
                  }
                  className='h-11 w-full'
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
              </div>
              <label className='text-muted-foreground flex cursor-pointer items-center gap-2.5 pb-2.5 text-sm font-normal select-none'>
                <Switch
                  checked={bindFree}
                  onCheckedChange={(checked) => setBindFree(checked === true)}
                />
                {t('Bind free plan')}
              </label>
            </div>

            <Button
              onClick={() => void submit()}
              disabled={
                submitting ||
                accountText.trim() === '' ||
                channelId === '' ||
                channels.length === 0
              }
              className='h-11 w-full gap-2 text-[15px] font-semibold'
            >
              {submitting ? (
                <Spinner className='h-4 w-4' />
              ) : (
                <Send className='h-4 w-4' />
              )}
              {submitting ? t('Submitting') : t('Submit')}
            </Button>

            <div className='border-t pt-4'>
              <div className='text-muted-foreground flex flex-wrap items-center gap-x-1 gap-y-1 text-[11px]'>
                <span className='mr-1 font-medium tracking-wider uppercase'>
                  {t('Pipeline')}
                </span>
                {STEP_LABEL_KEYS.map((labelKey, index) => (
                  <Fragment key={labelKey}>
                    {index > 0 ? (
                      <ChevronRight className='h-3 w-3 text-muted-foreground/40' />
                    ) : null}
                    <span>{t(labelKey)}</span>
                  </Fragment>
                ))}
              </div>
              <p className='text-muted-foreground/70 mt-1.5 text-xs'>
                {t('One submission runs five automated steps')}
              </p>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t('Jobs')}</CardTitle>
            <CardDescription>
              {t('Auto-refreshes every 2 seconds')}
            </CardDescription>
          </CardHeader>
          <CardContent className='space-y-4'>
            {jobs.length === 0 ? (
              <p className='text-muted-foreground text-sm'>{t('No jobs yet')}</p>
            ) : (
              <div className='rounded-md border'>
                <table className='w-full text-sm'>
                  <thead>
                    <tr className='text-muted-foreground border-b text-left text-xs'>
                      <th className='w-8 px-3 py-2' aria-label={t('Details')} />
                      <th className='px-3 py-2'>{t('Time')}</th>
                      <th className='px-3 py-2'>{t('Type')}</th>
                      <th className='px-3 py-2'>{t('Account')}</th>
                      <th className='px-3 py-2'>{t('Channel')}</th>
                      <th className='px-3 py-2'>{t('Pipeline')}</th>
                      <th className='px-3 py-2'>{t('Error')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {jobs.map((job) => (
                      <Fragment key={job.id}>
                        <tr
                          className='cursor-pointer border-b last:border-b-0 hover:bg-muted/40'
                          onClick={() => toggleExpanded(job.id)}
                        >
                          <td className='px-3 py-2'>
                            <ChevronDown
                              className={cn(
                                'h-4 w-4 text-muted-foreground transition-transform',
                                expandedId === job.id && 'rotate-180'
                              )}
                            />
                          </td>
                          <td className='px-3 py-2 whitespace-nowrap'>
                            {formatTime(job.created_at)}
                          </td>
                          <td className='px-3 py-2'>{modeLabel(job.account_mode)}</td>
                          <td className='px-3 py-2 font-mono text-xs'>
                            {job.masked_email}
                          </td>
                          <td className='px-3 py-2 whitespace-nowrap'>
                            #{job.channel_id}
                          </td>
                          <td className='px-3 py-2'>
                            <div className='flex flex-col gap-1'>
                              <MiniStepper steps={deriveSteps(job)} />
                              <span className='text-muted-foreground text-xs'>
                                {t(
                                  STATUS_LABEL_KEYS[job.status] ?? job.status
                                )}
                              </span>
                            </div>
                          </td>
                          <td
                            className='text-destructive max-w-48 truncate px-3 py-2 text-xs'
                            title={errorText(job)}
                          >
                            {job.error_class ? (
                              <span className='text-destructive'>
                                {errorText(job)}
                              </span>
                            ) : (
                              <span className='text-muted-foreground'>—</span>
                            )}
                          </td>
                        </tr>
                        {expandedId === job.id ? (
                          <tr className='border-b last:border-b-0'>
                            <td colSpan={7} className='bg-muted/20 px-4 py-4'>
                              <div className='space-y-4'>
                                <DetailStepper steps={deriveSteps(job)} />
                                {job.error_class ? (
                                  <div className='rounded-md border border-red-500/40 bg-red-500/5 px-3 py-2'>
                                    <p className='text-destructive text-sm'>
                                      {errorText(job)}
                                    </p>
                                    <p className='text-muted-foreground font-mono text-xs'>
                                      {job.error_class}
                                    </p>
                                  </div>
                                ) : null}
                                <dl className='text-muted-foreground grid grid-cols-2 gap-x-6 gap-y-1 text-xs md:grid-cols-4'>
                                  <div>
                                    <dt className='font-medium'>
                                      {t('SMS688 batch ID')}
                                    </dt>
                                    <dd className='font-mono'>
                                      {job.sms688_batch_id || '—'}
                                    </dd>
                                  </div>
                                  <div>
                                    <dt className='font-medium'>{t('Stage')}</dt>
                                    <dd className='font-mono'>
                                      {job.stage || '—'}
                                    </dd>
                                  </div>
                                  <div>
                                    <dt className='font-medium'>
                                      {t('Started at')}
                                    </dt>
                                    <dd>{formatTime(job.created_at)}</dd>
                                  </div>
                                  <div>
                                    <dt className='font-medium'>
                                      {t('Last activity')}
                                    </dt>
                                    <dd>{formatTime(job.updated_at)}</dd>
                                  </div>
                                </dl>
                              </div>
                            </td>
                          </tr>
                        ) : null}
                      </Fragment>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div className='flex items-center justify-between text-sm'>
              <span className='text-muted-foreground'>
                {t('{{page}} / {{count}}', {
                  page: page + 1,
                  count: pageCount,
                })}
                {total > 0 ? ` · ${total}` : ''}
              </span>
              <div className='flex gap-2'>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={page === 0}
                  onClick={() => setPage((current) => Math.max(0, current - 1))}
                >
                  {t('Previous page')}
                </Button>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={page + 1 >= pageCount}
                  onClick={() => setPage((current) => current + 1)}
                >
                  {t('Next page')}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

export { AccountAutomationContent }
