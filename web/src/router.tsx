import { createBrowserRouter } from 'react-router'

import { ChatPage, chatLoader } from '@/routes/chat'
import { HomePage, homeLoader } from '@/routes/home'
import { IntroPage, introLoader } from '@/routes/intro'
import { LoginPage, loginLoader } from '@/routes/login'
import { MatchesPage, matchesLoader } from '@/routes/matches'
import { MePage, meLoader } from '@/routes/me'
import { NotFoundPage } from '@/routes/notfound'
import { OnboardingPage, onboardingLoader } from '@/routes/onboarding'
import { PhotosPage, photosLoader } from '@/routes/photos'
import { ProfileEditPage, profileEditLoader } from '@/routes/profile-edit'
import { Root, RootError, rootLoader } from '@/routes/root'

/**
 * 页面清单见 §19.7。/me/preferences、/me/settings、/admin 还没有接口，
 * 先不建空壳页面：一个点进去什么都没有的入口比没有入口更糟。
 */
export const router = createBrowserRouter([
  {
    path: '/login',
    loader: loginLoader,
    Component: LoginPage,
    ErrorBoundary: RootError,
  },
  {
    path: '/onboarding',
    loader: onboardingLoader,
    Component: OnboardingPage,
    ErrorBoundary: RootError,
  },
  {
    // 有壳的那一层。loader 在这里 await /me，业务页在它之下才可能被渲染
    id: 'root',
    path: '/',
    loader: rootLoader,
    Component: Root,
    ErrorBoundary: RootError,
    children: [
      { index: true, loader: homeLoader, Component: HomePage },
      { path: 'intro/:id', loader: introLoader, Component: IntroPage },
      { path: 'matches', loader: matchesLoader, Component: MatchesPage },
      { path: 'chat/:id', loader: chatLoader, Component: ChatPage },
      { path: 'me', loader: meLoader, Component: MePage },
      { path: 'me/edit', loader: profileEditLoader, Component: ProfileEditPage },
      { path: 'me/photos', loader: photosLoader, Component: PhotosPage },
    ],
  },
  { path: '*', Component: NotFoundPage },
])
