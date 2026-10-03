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
import { describe, expect, test } from 'vitest'

import {
  getAdvancedCustomConverterDefaults,
  getAdvancedCustomConverterOptions,
  validateAdvancedCustomConfig,
} from '../advanced-custom'

describe('OpenAI Responses to Anthropic Messages converter', () => {
  test('is offered for the Responses incoming path only', () => {
    const responsesOptions = getAdvancedCustomConverterOptions('/v1/responses')
    const chatOptions = getAdvancedCustomConverterOptions(
      '/v1/chat/completions'
    )

    expect(responsesOptions.map((option) => option.value)).toContain(
      'openai_responses_to_claude_messages'
    )
    expect(chatOptions.map((option) => option.value)).not.toContain(
      'openai_responses_to_claude_messages'
    )
  })

  test('defaults to the Messages upstream with the x-api-key header', () => {
    expect(
      getAdvancedCustomConverterDefaults(
        'openai_responses_to_claude_messages',
        '/v1/responses'
      )
    ).toEqual({
      upstream_path: '/v1/messages',
      auth: { type: 'header', name: 'x-api-key', value: '{api_key}' },
    })
  })

  test('validates on /v1/responses and is rejected on another incoming path', () => {
    const route = {
      upstream_path: '/v1/messages',
      converter: 'openai_responses_to_claude_messages' as const,
    }

    expect(
      validateAdvancedCustomConfig({
        advanced_routes: [{ ...route, incoming_path: '/v1/responses' }],
      })
    ).toBeNull()
    expect(
      validateAdvancedCustomConfig({
        advanced_routes: [{ ...route, incoming_path: '/v1/chat/completions' }],
      })
    ).toEqual({
      routeIndex: 0,
      message: 'Converter does not match incoming path',
    })
  })
})
