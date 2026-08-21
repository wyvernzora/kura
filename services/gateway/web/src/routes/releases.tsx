import { createFileRoute } from '@tanstack/react-router';

import { ReleasesPage } from '@/components/ReleasesPage';

export const Route = createFileRoute('/releases')({
  component: ReleasesPage,
});
