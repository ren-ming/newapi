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
import { Check, Loader2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { PipelineStep, StepState } from './steps'

const DOT_CLASSES: Record<StepState, string> = {
  done: 'bg-emerald-500 border-emerald-500',
  active: 'bg-blue-500 border-blue-500 animate-pulse',
  error: 'bg-red-500 border-red-500 ring-2 ring-red-500/30',
  pending: 'bg-transparent border-gray-300 dark:border-gray-600',
}

const CONNECTOR_DONE = 'bg-emerald-400'
const CONNECTOR_IDLE = 'bg-gray-200 dark:bg-gray-700'

/** Compact dot chain rendered inside the jobs table row. */
export function MiniStepper({ steps }: { steps: PipelineStep[] }) {
  const { t } = useTranslation()
  return (
    <div className='flex items-center gap-1' aria-label={t('Pipeline progress')}>
      {steps.map((step, index) => (
        <span key={step.key} className='flex items-center gap-1'>
          {index > 0 ? (
            <span
              className={cn(
                'h-0.5 w-3 rounded-full',
                step.state === 'pending' ? CONNECTOR_IDLE : CONNECTOR_DONE
              )}
            />
          ) : null}
          <span
            title={t(step.labelKey)}
            className={cn('h-2 w-2 rounded-full border', DOT_CLASSES[step.state])}
          />
        </span>
      ))}
    </div>
  )
}

const ICON_CLASSES: Record<StepState, string> = {
  done: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/40',
  active: 'bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/40',
  error: 'bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/40',
  pending: 'bg-transparent text-gray-400 dark:text-gray-600 border-gray-300 dark:border-gray-600',
}

const STATE_LABEL_KEYS: Record<StepState, string> = {
  done: 'Done',
  active: 'In progress',
  error: 'Failed',
  pending: 'Pending',
}

/** Larger labeled stepper rendered in the expanded job detail panel. */
export function DetailStepper({ steps }: { steps: PipelineStep[] }) {
  const { t } = useTranslation()
  return (
    <div className='flex w-full items-start'>
      {steps.map((step, index) => (
        <div key={step.key} className='flex min-w-0 flex-1 items-start'>
          <div className='flex min-w-14 flex-col items-center gap-1'>
            <span
              className={cn(
                'flex h-7 w-7 items-center justify-center rounded-full border',
                ICON_CLASSES[step.state]
              )}
              aria-label={`${t(step.labelKey)}: ${t(STATE_LABEL_KEYS[step.state])}`}
            >
              {step.state === 'done' ? (
                <Check className='h-4 w-4' />
              ) : step.state === 'active' ? (
                <Loader2 className='h-4 w-4 animate-spin' />
              ) : step.state === 'error' ? (
                <X className='h-4 w-4' />
              ) : (
                <span className='text-xs font-medium'>{index + 1}</span>
              )}
            </span>
            <span className='text-center text-xs leading-tight'>
              {t(step.labelKey)}
            </span>
            <span
              className={cn(
                'text-center text-[10px] leading-tight',
                step.state === 'error'
                  ? 'text-red-600 dark:text-red-400'
                  : 'text-muted-foreground'
              )}
            >
              {t(STATE_LABEL_KEYS[step.state])}
            </span>
          </div>
          {index < steps.length - 1 ? (
            <span
              className={cn(
                'mt-3.5 h-0.5 flex-1 rounded-full',
                steps[index + 1].state === 'pending'
                  ? CONNECTOR_IDLE
                  : CONNECTOR_DONE
              )}
            />
          ) : null}
        </div>
      ))}
    </div>
  )
}
