<template>
  <div>
    <SectionLabel style="margin-bottom: 12px;">{{ $t('objects.tls') }}</SectionLabel>
    <Field :label="$t('template')">
      <Select v-model="tlsId">
        <option :value="0">{{ $t('none') }}</option>
        <option v-for="c in (tlsConfigs ?? [])" :key="c.id" :value="c.id">{{ c.name }}</option>
      </Select>
    </Field>
    <!-- OpenConnect and OpenVPN define their own TLS options rather than using
         sing-box's, so only the overlapping fields are carried over. The ones
         that would silently not apply are called out instead of dropped in
         silence. -->
    <MHint v-if="unsupported.length > 0">
      {{ $t('tls.endpointUnsupported', { fields: unsupported.join(', ') }) }}
    </MHint>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Select from '@/components/ui/Select.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import MHint from '@/components/ui/MHint.vue'

// Fields that carry real behaviour but have no equivalent on a given endpoint
// type. Must stay in step with unsupportedEndpointTLS in the panel core.
const unsupportedByType: Record<string, { side: 'server' | 'client'; fields: string[] }> = {
  'openvpn-server': { side: 'server', fields: ['certificate_provider', 'reality', 'ech', 'client_certificate_public_key_sha256'] },
  'openvpn-client': { side: 'client', fields: ['insecure', 'reality', 'ech', 'certificate_public_key_sha256'] },
  'openconnect': { side: 'client', fields: ['reality', 'ech', 'certificate_public_key_sha256'] },
}

const props = defineProps<{ endpoint: any; tlsConfigs?: any[] }>()

const tlsId = computed({
  get: (): number => props.endpoint.tls_id ?? 0,
  set: (v: number) => { props.endpoint.tls_id = v },
})

const unsupported = computed((): string[] => {
  const rule = unsupportedByType[props.endpoint.type]
  if (!rule || !props.endpoint.tls_id) return []
  const selected = (props.tlsConfigs ?? []).find((t: any) => t.id === props.endpoint.tls_id)
  if (!selected) return []
  const side = selected[rule.side]
  if (!side) return []
  return rule.fields.filter((f) => {
    const value = side[f]
    if (value == undefined || value === false) return false
    if (Array.isArray(value)) return value.length > 0
    if (typeof value === 'object') return value.enabled !== false
    return true
  })
})
</script>
