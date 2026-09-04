<template>
  <div>
    <SectionLabel style="margin-bottom: 12px;">TProxy</SectionLabel>
    <Network :data="inbound" :mb="15" />
    <UdpNat :data="inbound" />
  </div>
</template>

<script lang="ts" setup>
import SectionLabel from '@/components/ui/SectionLabel.vue'
import Network from '../out/Network.vue'
import UdpNat from '../out/UdpNat.vue'

defineProps<{ inbound: any }>()
</script>
