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
along with this program. If not at <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'

interface AutomationAccount {
  id: string
  masked_email: string
  channel_id: number
  status: string
  stage?: string
  error_class?: string
}

interface AutomationBatch {
  id: string
  status: string
  accounts: AutomationAccount[]
  error_class?: string
  created_at: string
  updated_at: string
}

const statusTone = (status: string): 'secondary' | 'destructive' | 'default' => {
  if (status === 'succeeded' || status === 'completed') {
    return 'secondary'
  }
  if (
    status.includes('failed') ||
    status.includes('invalid') ||
    status.includes('expired') ||
    status.includes('cancelled')
  ) {
    return 'destructive'
  }
  return 'default'
}

function AccountAutomationContent() {
  const { t } = useTranslation()
  const [accountText, setAccountText] = useState('')
  const [bindFree, setBindFree] = useState(false)
  const [batches, setBatches] = useState<AutomationBatch[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const refresh = useCallback(async () => {
    try {
      const { data } = await api.get<AutomationBatch[]>(
        '/api/account-automation/batches'
      )
      setBatches(Array.isArray(data) ? data : [])
    } catch {
      // Polling failures are silent; the next tick retries.
    }
  }, [])

  useEffect(() => {
    void refresh()
    const timer = setInterval(() => void refresh(), 2000)
    return () => clearInterval(timer)
  }, [refresh])

  const submit = async () => {
    setSubmitting(true)
    setError('')
    setNotice('')
    try {
      await api.post('/api/account-automation/batches', {
        account_text: accountText,
        bind_free: bindFree,
      })
      setAccountText('')
      setNotice(t('Batch submitted'))
      await refresh()
    } catch (submitError: unknown) {
      const message =
        (submitError as { response?: { data?: { error?: string } } })?.response
          ?.data?.error ?? (submitError as Error).message
      setError(String(message))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Account Automation')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'Submit account lines as channel_id|email----password; CPA credentials are written into the Codex channel automatically'
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
              {t('One account per line, format: channel_id|email----password')}
            </CardDescription>
          </CardHeader>
          <CardContent className='space-y-4'>
            <Textarea
              value={accountText}
              onChange={(event) => setAccountText(event.target.value)}
              rows={5}
              placeholder={'57|user@example.com----password'}
              spellCheck={false}
            />
            <div className='flex items-center justify-between'>
              <label className='flex items-center gap-2 text-sm'>
                <Checkbox
                  checked={bindFree}
                  onCheckedChange={(checked) => setBindFree(checked === true)}
                />
                {t('Bind free plan')}
              </label>
              <Button
                onClick={() => void submit()}
                disabled={submitting || accountText.trim() === ''}
              >
                {submitting ? <Spinner className='h-4 w-4' /> : null}
                {t('Submit')}
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t('Batches')}</CardTitle>
            <CardDescription>{t('Auto-refreshes every 2 seconds')}</CardDescription>
          </CardHeader>
          <CardContent className='space-y-4'>
            {batches.length === 0 ? (
              <p className='text-muted-foreground text-sm'>{t('No batches yet')}</p>
            ) : (
              batches.map((batch) => (
                <div key={batch.id} className='rounded-md border p-3'>
                  <div className='mb-2 flex flex-wrap items-center gap-2 text-sm'>
                    <span className='font-mono text-xs'>{batch.id}</span>
                    <Badge variant={statusTone(batch.status)}>{batch.status}</Badge>
                    {batch.error_class ? (
                      <Badge variant='destructive'>{batch.error_class}</Badge>
                    ) : null}
                  </div>
                  <div className='space-y-1'>
                    {batch.accounts.map((account) => (
                      <div
                        key={account.id}
                        className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs'
                      >
                        <span>{account.masked_email}</span>
                        <span>#{account.channel_id}</span>
                        <Badge variant={statusTone(account.status)}>
                          {account.status}
                        </Badge>
                        {account.error_class ? (
                          <Badge variant='destructive'>
                            {account.error_class}
                          </Badge>
                        ) : null}
                      </div>
                    ))}
                  </div>
                </div>
              ))
            )}
          </CardContent>
        </Card>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

export { AccountAutomationContent }
